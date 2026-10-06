package prtriage_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/prtriage"
)

func refGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func refWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// refRecord is a one-resolution record resolving id.
func refRecord(id string) string {
	return `[[resolution]]
id = "` + id + `"
kind = "conversation"
pr = 7
url = "https://github.com/acme/w/pull/7#c"
author = "bob"
verdict = "reject"
reason = "no"
at = "x.go:1"
digest = "` + strings.Repeat("ab", 32) + `"
`
}

// refRepo is a git repo with branches phase/a (a committed record resolving
// c1 and c2) and phase/b (no record), checked out on phase/b.
func refRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	refGit(t, dir, "init", "-q", "-b", "main")
	for _, kv := range [][2]string{{"user.email", "t@example.com"}, {"user.name", "T"}, {"commit.gpgsign", "false"}, {"gc.auto", "0"}} {
		refGit(t, dir, "config", kv[0], kv[1])
	}
	refWrite(t, filepath.Join(dir, "README.md"), "base\n")
	refGit(t, dir, "add", ".")
	refGit(t, dir, "commit", "-q", "-m", "base")
	refGit(t, dir, "checkout", "-q", "-b", "phase/a")
	refWrite(t, filepath.Join(dir, ".dross", "phases", "a", "plan.toml"), "[phase]\nid = \"a\"\n")
	refWrite(t, filepath.Join(dir, ".dross", "phases", "a", prtriage.File), refRecord("c1")+"\n"+refRecord("c2"))
	refGit(t, dir, "add", ".")
	refGit(t, dir, "commit", "-q", "-m", "triage a")
	refGit(t, dir, "checkout", "-q", "main")
	refGit(t, dir, "checkout", "-q", "-b", "phase/b")
	refWrite(t, filepath.Join(dir, ".dross", "phases", "b", "plan.toml"), "[phase]\nid = \"b\"\n")
	refGit(t, dir, "add", ".")
	refGit(t, dir, "commit", "-q", "-m", "plan b")
	return dir
}

func recIDs(rec prtriage.Record) string {
	var out []string
	for _, r := range rec.Resolution {
		out = append(out, r.ID)
	}
	return strings.Join(out, ",")
}

func TestLoadForPhaseOffBranchReadsTheRef(t *testing.T) {
	dir := refRepo(t)
	rec, known := prtriage.LoadForPhase(dir, "a")
	if !known || recIDs(rec) != "c1,c2" {
		t.Errorf("on phase/b, phase a's record = %q (known %v), want the committed c1,c2", recIDs(rec), known)
	}
	if rec, known := prtriage.LoadForPhase(dir, "b"); !known || len(rec.Resolution) != 0 {
		t.Errorf("phase b, current, with no record = %q (known %v), want known and empty", recIDs(rec), known)
	}
}

// TestLoadForPhaseBranchWithoutRecord: a shipped phase nobody has triaged
// yet, seen from another branch, is known and empty — every comment counts.
func TestLoadForPhaseBranchWithoutRecord(t *testing.T) {
	dir := refRepo(t)
	refGit(t, dir, "branch", "-q", "phase/c", "main")
	rec, known := prtriage.LoadForPhase(dir, "c")
	if !known || len(rec.Resolution) != 0 {
		t.Errorf("phase/c with no record, from phase/b = %q (known %v), want known and empty", recIDs(rec), known)
	}
}

func TestLoadForPhaseOnBranchReadsTheWorkingTree(t *testing.T) {
	dir := refRepo(t)
	refGit(t, dir, "checkout", "-q", "phase/a")
	refWrite(t, filepath.Join(dir, ".dross", "phases", "a", prtriage.File), refRecord("c1")+"\n"+refRecord("c2")+"\n"+refRecord("c3"))
	rec, known := prtriage.LoadForPhase(dir, "a")
	if !known || recIDs(rec) != "c1,c2,c3" {
		t.Errorf("on phase/a with an uncommitted resolution = %q (known %v), want c1,c2,c3", recIDs(rec), known)
	}
}

func TestLoadForPhaseUnknown(t *testing.T) {
	dir := refRepo(t)
	if _, known := prtriage.LoadForPhase(dir, "nope"); known {
		t.Error("a phase with no branch read as known")
	}
	refGit(t, dir, "checkout", "-q", "phase/a")
	refWrite(t, filepath.Join(dir, ".dross", "phases", "a", prtriage.File), "body = \"pasted\"\n")
	refGit(t, dir, "add", ".")
	refGit(t, dir, "commit", "-q", "-m", "break it")
	if _, known := prtriage.LoadForPhase(dir, "a"); known {
		t.Error("a malformed working-tree record read as known")
	}
	refGit(t, dir, "checkout", "-q", "phase/b")
	if _, known := prtriage.LoadForPhase(dir, "a"); known {
		t.Error("a malformed committed record read as known")
	}
	if _, known := prtriage.LoadForPhase(t.TempDir(), "a"); known {
		t.Error("a directory that is no repo read as known")
	}
}
