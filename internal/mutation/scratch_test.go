package mutation

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestScratchIsolatesFromShared: the run gets a directory of its own, under the
// base the caller chose. Landing in os.TempDir() instead would put a 60 GB
// build cache on helicon's RAM-backed /tmp, which is the outage this exists to
// prevent.
func TestScratchIsolatesFromShared(t *testing.T) {
	base := t.TempDir()

	s, err := newScratch(base, []string{"GOCACHE"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Remove() })

	if s.Dir == "" {
		t.Fatal("no scratch directory was created")
	}
	info, err := os.Stat(s.Dir)
	if err != nil {
		t.Fatalf("the scratch dir does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("the scratch is not a directory")
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a fresh scratch is not empty: %v", entries)
	}
	if !strings.HasPrefix(s.Dir, base) {
		t.Errorf("scratch %q is not under the requested base %q", s.Dir, base)
	}
	if strings.HasPrefix(s.Dir, os.TempDir()) && !strings.HasPrefix(base, os.TempDir()) {
		t.Errorf("scratch %q landed in the OS temp dir rather than the chosen base", s.Dir)
	}
}

// TestScratchAssignsEveryVar: every declared variable points at the SAME
// directory, in declaration order. One dir because they are all names for one
// cache; in order because the assignments are emitted into a script a reader
// diffs between runs.
func TestScratchAssignsEveryVar(t *testing.T) {
	s, err := newScratch(t.TempDir(), []string{"GOCACHE", "GOMODCACHE"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Remove() })

	got := s.Assignments()
	want := []string{"GOCACHE=" + s.Dir, "GOMODCACHE=" + s.Dir}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("assignments = %v, want %v", got, want)
	}
}

// TestScratchRemoveWipes: a wipe that left the contents behind would be the
// whole bug, quietly.
func TestScratchRemoveWipes(t *testing.T) {
	s, err := newScratch(t.TempDir(), []string{"GOCACHE"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	dir := s.Dir
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "archive.a"), bytes.Repeat([]byte("x"), 1<<16), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the scratch survived the wipe: %v", err)
	}
}

// TestScratchRemoveIsIdempotent: every exit path calls Remove, so a deferred
// wipe and an explicit one must not fight.
func TestScratchRemoveIsIdempotent(t *testing.T) {
	s, err := newScratch(t.TempDir(), []string{"GOCACHE"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(); err != nil {
		t.Fatalf("first Remove: %v", err)
	}
	if err := s.Remove(); err != nil {
		t.Errorf("second Remove: %v", err)
	}
	if got := s.Assignments(); got != nil {
		t.Errorf("a removed scratch still reports assignments: %v", got)
	}
}

// TestScratchRemoveNeverFailsTheRun: the locked wipe_policy, both halves. A
// cleanup error must not destroy a measurement that already completed — and it
// must not be silent, because silence is exactly how the cache reached 399 GB.
func TestScratchRemoveNeverFailsTheRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not block removal the same way on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only parent does not block removal")
	}
	base := t.TempDir()
	var log bytes.Buffer
	s, err := newScratch(base, []string{"GOCACHE"}, &log)
	if err != nil {
		t.Fatal(err)
	}
	dir := s.Dir
	// A read-only PARENT is what blocks the unlink; the entry cannot be removed
	// from a directory that is not writable.
	if err := os.Chmod(base, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(base, 0o700) })

	if err := s.Remove(); err != nil {
		t.Errorf("a failed wipe took the run down with it: %v", err)
	}
	out := log.String()
	if out == "" {
		t.Fatal("a failed wipe was silent — a leak nobody sees is what filled the disk")
	}
	if !strings.Contains(out, dir) {
		t.Errorf("the report does not name the path that leaked: %q", out)
	}
}

// TestScratchNoVarsIsNoOp: a stack whose profile declares no cache var must be
// left exactly as it is today — no directory, no assignments, nothing to wipe.
func TestScratchNoVarsIsNoOp(t *testing.T) {
	base := t.TempDir()
	s, err := newScratch(base, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if s.Dir != "" {
		t.Errorf("a stack with no declared cache var got a scratch dir: %q", s.Dir)
	}
	if got := s.Assignments(); got != nil {
		t.Errorf("assignments = %v, want none", got)
	}
	if err := s.Remove(); err != nil {
		t.Errorf("Remove on a no-op scratch: %v", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a no-op scratch created %v under the base", entries)
	}
}

// TestRemoteScratchDirIsUnderWorkdir: a remote scratch belongs on the volume the
// operator granted, not on whatever the host calls temp. helicon's /tmp is a
// 32 GB tmpfs in RAM, shared with a running LLM.
func TestRemoteScratchDirIsUnderWorkdir(t *testing.T) {
	const workdir = "/home/rivil/dross"
	dir := remoteScratchDir(workdir)

	if !strings.HasPrefix(dir, workdir+"/") {
		t.Errorf("remote scratch %q is not under the granted workdir %q", dir, workdir)
	}
	if strings.HasPrefix(dir, "/tmp") || strings.HasPrefix(dir, "/var/tmp") {
		t.Errorf("remote scratch %q landed in a host temp path", dir)
	}
	got := remoteAssignments(dir, []string{"GOCACHE", "TMPDIR"})
	want := []string{"GOCACHE=" + dir, "TMPDIR=" + dir}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("remote assignments = %v, want %v", got, want)
	}
	if remoteAssignments(dir, nil) != nil {
		t.Error("no vars must yield no assignments")
	}
	if remoteAssignments("", []string{"GOCACHE"}) != nil {
		t.Error("no dir must yield no assignments")
	}
}
