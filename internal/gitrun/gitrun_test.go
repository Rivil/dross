package gitrun

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubGit puts a fake `git` first on PATH. It echoes its whole argv on one
// line, and for a `fetch` or `status` also prints CANARY to stdout and stderr
// and exits 1 — standing in for a git whose failure output quotes the repo.
func stubGit(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub git is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = fetch ] || [ \"$a\" = status ]; then echo CANARY; echo CANARY >&2; exit 1; fi\n" +
		"done\n" +
		"echo \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// captureStderr swaps Stderr for the test.
func captureStderr(t *testing.T) *strings.Builder {
	t.Helper()
	var b strings.Builder
	prev := Stderr
	Stderr = &b
	t.Cleanup(func() { Stderr = prev })
	return &b
}

// realRepo is a git work tree with one commit on main.
func realRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"config", "gc.auto", "0"},
	} {
		if err := Quiet(dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "a.go"}, {"commit", "-q", "-m", "init"}} {
		if err := Run(dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	return dir
}

// TestRunFailureOutputGoesToStderr: a failing Run's error carries git's exit
// status and never its output; the output lands on Stderr labelled with the
// verb.
func TestRunFailureOutputGoesToStderr(t *testing.T) {
	stubGit(t)
	stderr := captureStderr(t)
	err := Run(t.TempDir(), "fetch", "origin")
	if err == nil {
		t.Fatal("a failing git reported success")
	}
	if strings.Contains(err.Error(), "CANARY") {
		t.Errorf("git's output reached the error: %q", err)
	}
	if !strings.Contains(stderr.String(), "git fetch:\n") || !strings.Contains(stderr.String(), "CANARY") {
		t.Errorf("Stderr = %q, want git's output labelled \"git fetch:\"", stderr.String())
	}
	if ExitCode(err) != 1 {
		t.Errorf("ExitCode = %d, want 1", ExitCode(err))
	}
}

// TestQuietFailurePrintsNothing: Quiet's output goes nowhere — not the error,
// not Stderr.
func TestQuietFailurePrintsNothing(t *testing.T) {
	stubGit(t)
	stderr := captureStderr(t)
	err := Quiet(t.TempDir(), "fetch")
	if err == nil {
		t.Fatal("a failing git reported success")
	}
	if strings.Contains(err.Error(), "CANARY") || strings.Contains(stderr.String(), "CANARY") {
		t.Errorf("Quiet leaked git's output: err=%q stderr=%q", err, stderr.String())
	}
}

// TestEveryVerbRunsInDirAndIsRecorded: each verb's argv starts `-C <dir>`, and
// ArgvRecorder sees the argv without it.
func TestEveryVerbRunsInDirAndIsRecorded(t *testing.T) {
	stubGit(t)
	var recorded [][]string
	prev := ArgvRecorder
	ArgvRecorder = func(args []string) { recorded = append(recorded, append([]string(nil), args...)) }
	t.Cleanup(func() { ArgvRecorder = prev })

	dir := t.TempDir()
	want := "-C " + dir + " rev-parse HEAD"
	if got, err := Trim(dir, "rev-parse", "HEAD"); err != nil || got != want {
		t.Errorf("Trim = %q, %v; want %q", got, err, want)
	}
	if got, err := Raw(dir, "rev-parse", "HEAD"); err != nil || got != want+"\n" {
		t.Errorf("Raw = %q, %v; want %q", got, err, want+"\n")
	}
	if got, err := Read(dir, "rev-parse", "HEAD"); err != nil || got != want {
		t.Errorf("Read = %q, %v; want %q", got, err, want)
	}
	if err := Run(dir, "rev-parse", "HEAD"); err != nil {
		t.Errorf("Run: %v", err)
	}
	if err := Quiet(dir, "rev-parse", "HEAD"); err != nil {
		t.Errorf("Quiet: %v", err)
	}
	// Read records once, through Raw.
	if len(recorded) != 5 {
		t.Fatalf("recorded %d argvs, want 5: %v", len(recorded), recorded)
	}
	for _, r := range recorded {
		if strings.Join(r, " ") != "rev-parse HEAD" {
			t.Errorf("recorded %q, want the argv without -C", r)
		}
	}
}

// TestRawKeepsWhatReadTrims: a porcelain status's leading space is the status
// column; Raw keeps it and Read trims it.
func TestRawKeepsWhatReadTrims(t *testing.T) {
	dir := realRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a // changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := Raw(dir, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, " M a.go") {
		t.Errorf("Raw = %q, want the leading status column kept", raw)
	}
	read, err := Read(dir, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if read != "M a.go" {
		t.Errorf("Read = %q, want it trimmed", read)
	}
}

// TestExitCodeReadsAncestry: a non-ancestor merge-base exits 1, which is an
// answer; git not running at all is -1, which is not.
func TestExitCodeReadsAncestry(t *testing.T) {
	dir := realRepo(t)
	if err := Run(dir, "checkout", "-q", "-b", "side"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "b.go"}, {"commit", "-q", "-m", "side"}} {
		if err := Run(dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := Quiet(dir, "merge-base", "--is-ancestor", "main", "side"); ExitCode(err) != 0 {
		t.Errorf("main is an ancestor of side, got %v (ExitCode %d)", err, ExitCode(err))
	}
	err := Quiet(dir, "merge-base", "--is-ancestor", "side", "main")
	if ExitCode(err) != 1 {
		t.Errorf("side is not an ancestor of main: ExitCode = %d (%v), want 1", ExitCode(err), err)
	}
	t.Setenv("PATH", "")
	if got := ExitCode(Quiet(dir, "status")); got != -1 {
		t.Errorf("with no git on PATH ExitCode = %d, want -1", got)
	}
}

// TestShortSHADegradesToNogit covers ShortSHA's failure arm. It is documented
// as best-effort — a run must not fail because the tree is not a git repo — so
// the fallback is the whole contract. Inverted, a real repo would be labelled
// "nogit" and every run id would collide with every other.
func TestShortSHADegradesToNogit(t *testing.T) {
	// A bare temp dir is not a git repo, so rev-parse fails.
	if got := ShortSHA(t.TempDir()); got != "nogit" {
		t.Errorf("ShortSHA(non-repo) = %q, want %q", got, "nogit")
	}

	// And in a real repo it returns something that is NOT the fallback, so the
	// guard is genuinely conditional. This test file lives inside this repo.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got := ShortSHA(cwd)
	if got == "nogit" {
		t.Errorf("ShortSHA(%q) = nogit inside a real git repo", cwd)
	}
	if strings.ContainsAny(got, " \n\t") {
		t.Errorf("ShortSHA = %q, want git's output trimmed", got)
	}
}

// TestNormalizeSHAFallsBackOnEmpty is the other half: git can exit 0 and print
// nothing. An empty run-id component would make two runs share a directory name
// and silently clobber each other, which NewRun's suffixing then papers over.
func TestNormalizeSHAFallsBackOnEmpty(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "nogit"},
		{"   ", "nogit"},
		{"\n\t ", "nogit"},
		{"abc1234\n", "abc1234"},
		{"  abc1234  ", "abc1234"},
	}
	for _, tc := range cases {
		if got := NormalizeSHA(tc.in); got != tc.want {
			t.Errorf("NormalizeSHA(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRawKeepsNULSeparatedNamesWithSpaces: remote's rsync exclude list reads
// `ls-files -z` through Raw and splits on NUL. A name with a space must come
// back whole — the exclude would otherwise name a path that does not exist and
// ship the ignored one.
func TestRawKeepsNULSeparatedNamesWithSpaces(t *testing.T) {
	dir := realRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("with space/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "with space"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "with space", "x.o"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Raw(dir, "ls-files", "--others", "--ignored", "--exclude-standard",
		"--directory", "--no-empty-directory", "-z")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			names = append(names, p)
		}
	}
	if len(names) != 1 || names[0] != "with space/" {
		t.Errorf("ignored names = %q, want exactly [\"with space/\"]", names)
	}
}
