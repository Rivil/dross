package mutation

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	// BEFORE the mkdir, and before anything is written. A refusal that had
	// already created directories would leave the volume it is protecting
	// slightly worse off than when it declined to use it.
	if err := checkScratchSpace(base); err != nil {
		return nil, err
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

// scratchDirFor is where a run's scratch lives: BESIDE the tree, on the same
// volume, never inside it and never in the machine's default temp.
//
// Two constraints, and only one placement satisfies both.
//
// Not the default temp, because that is whatever the machine happened to
// configure — on helicon it is a 32 GB tmpfs in RAM shared with a running LLM,
// where a build cache is an outage rather than a slowdown.
//
// The tree's parent is the default because it is the best AVAILABLE guess, not
// because it is known to be right. This used to claim the parent was "the
// volume the operator deliberately chose for this work", and on the host it
// names that was false for three months: /home there is part of the 75 GB root
// LV, while the 300 GB volume provisioned for exactly this workload sat
// elsewhere. A run filled root at 1.3 GB/min and came within about two minutes
// of wedging a host serving unrelated services off the same volume — while
// every guard watched the volume that was idle, because the two cache
// variables dross does NOT override still pointed at it.
//
// So the guess stays, and ScratchBaseOverride exists for when it is wrong.
//
// Not inside the tree, which the first live run proved the hard way. The
// exported TMPDIR is what gremlins copies the module through, so every
// t.TempDir() in the suite became a child of the repo — and every test that
// asserts dross cannot find a root from a scratch directory started finding
// one, because FindRoot walked up into the very repo under test. Nine tests
// went red on the host and green locally for a reason that had nothing to do
// with the code. A cache inside the tree also lands in git status and in the
// adapters' own file scans.
//
// Keyed by the tree's base name so two repos on one machine do not share.
func scratchDirFor(tree string) string {
	tree = filepath.Clean(tree)
	if base := ScratchBaseOverride(); base != "" {
		return filepath.Join(base, ".dross-cache", filepath.Base(tree))
	}
	return filepath.Join(filepath.Dir(tree), ".dross-cache", filepath.Base(tree))
}

// ScratchBaseEnv is the variable an operator sets to put this machine's scratch
// on a chosen volume without relocating the checkout.
//
// It exists because the only lever used to be where the tree happens to live,
// which couples cache placement to an unrelated decision — and got that
// placement wrong on the one host it was tested against.
const ScratchBaseEnv = "DROSS_SCRATCH_BASE"

// ScratchBaseOverride returns the configured base, or "" for the default.
//
// A relative value is IGNORED rather than resolved. The scratch is created and
// later removed wholesale, and resolving a relative path against a working
// directory that varies per adapter is how an rm -rf finds somewhere nobody
// meant. An absolute path or nothing.
func ScratchBaseOverride() string {
	base := strings.TrimSpace(os.Getenv(ScratchBaseEnv))
	if base == "" || !filepath.IsAbs(base) {
		return ""
	}
	return filepath.Clean(base)
}

// remoteScratchDir is scratchDirFor for a path on another machine. Named
// separately because the remote path is never created by this process and never
// resolved against this filesystem.
func remoteScratchDir(workdir string) string { return scratchDirFor(workdir) }

// parentOf is filepath.Dir, named so scratch_space.go's walk reads as intent
// rather than as path manipulation.
func parentOf(path string) string { return filepath.Dir(path) }

func (s *scratch) report(format string, args ...any) {
	if s == nil || s.reporter == nil {
		return
	}
	fmt.Fprintf(s.reporter, format, args...)
}
