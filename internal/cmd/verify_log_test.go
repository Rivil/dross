package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/runlog"
	"github.com/Rivil/dross/internal/verify"
)

// streamAdapter is a mutation leg that prints 300 per-mutant lines to its
// stream (stderr when none is set) and reports one kill per dispatched file.
type streamAdapter struct {
	name   string
	ext    string
	stream *runlog.Log
	ran    bool
}

func (s *streamAdapter) Name() string              { return s.name }
func (s *streamAdapter) Supports(file string) bool { return strings.HasSuffix(file, s.ext) }
func (s *streamAdapter) SetStream(l *runlog.Log)   { s.stream = l }
func (s *streamAdapter) Run(files []string) (*mutation.Report, error) {
	s.ran = true
	var w io.Writer = os.Stderr
	if s.stream != nil {
		w = s.stream
	}
	for i := 0; i < 300; i++ {
		fmt.Fprintf(w, "LIVED stream-mutant-%d\n", i)
	}
	rep := &mutation.Report{Tool: s.name, Files: map[string]mutation.FileStat{}}
	for _, f := range files {
		rep.Files[f] = mutation.FileStat{Killed: 1}
		rep.Killed++
	}
	rep.Score = 1
	return rep, nil
}

func countStreamMutants(s string) int { return strings.Count(s, "stream-mutant-") }

func useAdapters(t *testing.T, adapters ...mutation.Adapter) {
	t.Helper()
	prev := configuredAdaptersFn
	configuredAdaptersFn = func(*project.Project, string, bool, string, remote.WaitPolicy) ([]mutation.Adapter, mutationTuning, error) {
		return adapters, mutationTuning{}, nil
	}
	t.Cleanup(func() { configuredAdaptersFn = prev })
}

func withVerifyTTY(t *testing.T, tty bool) {
	t.Helper()
	orig := verifyStdoutIsTTY
	t.Cleanup(func() { verifyStdoutIsTTY = orig })
	verifyStdoutIsTTY = func() bool { return tty }
}

func verifyLogs(t *testing.T, repo, pattern string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repo, ".dross", "logs", pattern))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func runVerify(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut string
	var err error
	errOut = captureStderr(t, func() {
		out = captureStdout(t, func() { err = runCmd(t, Verify(), args...) })
	})
	return out, errOut, err
}

func readLog(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAttachedLegLogsStream(t *testing.T) {
	const id = "01-legs"
	dir := lockVerifyRepo(t, id)
	useAdapters(t, &streamAdapter{name: "gremlins", ext: ".go"})
	withVerifyTTY(t, false)
	out, errOut, err := runVerify(t, id)
	if err != nil {
		t.Fatal(err)
	}
	if n := countStreamMutants(out + errOut); n != 0 {
		t.Errorf("%d per-mutant lines reached stdout or stderr", n)
	}
	logs := verifyLogs(t, dir, "verify-"+id+"-gremlins-*.log")
	if len(logs) != 1 {
		t.Fatalf("verify logs %v, want one verify-%s-gremlins-*.log", logs, id)
	}
	if n := countStreamMutants(readLog(t, logs[0])); n != 300 {
		t.Errorf("the leg log holds %d of the 300 lines", n)
	}
	// Base name, not the whole path: the temp root resolves through a symlink
	// on some hosts (/var → /private/var).
	if !strings.Contains(out, "verify log (gremlins): /") || !strings.Contains(out, filepath.Base(logs[0])) {
		t.Errorf("the leg did not print its log path %s\nstdout:\n%s\nstderr:\n%.600s", logs[0], out, errOut)
	}
}

func TestAdaptersAreStreamers(t *testing.T) {
	for _, a := range []mutation.Adapter{&mutation.Gremlins{}, &mutation.Stryker{}, &mutation.StrykerNet{}} {
		if _, ok := a.(mutation.Streamer); !ok {
			t.Errorf("%T does not satisfy mutation.Streamer — its stream would still reach the conversation", a)
		}
	}
}

func TestTTYLegStillStreams(t *testing.T) {
	const id = "01-legs-tty"
	dir := lockVerifyRepo(t, id)
	useAdapters(t, &streamAdapter{name: "gremlins", ext: ".go"})
	withVerifyTTY(t, true)
	_, errOut, err := runVerify(t, id)
	if err != nil {
		t.Fatal(err)
	}
	if n := countStreamMutants(errOut); n != 300 {
		t.Errorf("on a terminal stderr holds %d of the 300 lines", n)
	}
	logs := verifyLogs(t, dir, "verify-"+id+"-gremlins-*.log")
	if len(logs) != 1 || countStreamMutants(readLog(t, logs[0])) != 300 {
		t.Errorf("on a terminal the log must still hold every line: %v", logs)
	}
}

func TestVerifyIdleLegOpensNoLog(t *testing.T) {
	const id = "01-legs-idle"
	dir := lockVerifyRepo(t, id)
	g := &streamAdapter{name: "gremlins", ext: ".go"}
	s := &streamAdapter{name: "stryker", ext: ".ts"}
	useAdapters(t, g, s)
	withVerifyTTY(t, false)
	if _, _, err := runVerify(t, id); err != nil {
		t.Fatal(err)
	}
	if s.ran {
		t.Fatal("fixture: the stryker leg was dispatched with no .ts file in scope")
	}
	if logs := verifyLogs(t, dir, "verify-*.log"); len(logs) != 1 {
		t.Fatalf("verify logs %v, want exactly one — an idle leg opens none", logs)
	}
}

func TestCollectFetchesHostLog(t *testing.T) {
	const id = "collect-log"
	dir := collectRepo(t, id)
	var asked string
	detachFetchLog = func(tg remote.Target, runDir, local string) error {
		asked = tg.Host + ":" + runDir
		return os.WriteFile(local, []byte("HOST-LOG-MARK\n"), 0o644)
	}
	var err error
	out := captureStdout(t, func() { err = collectDetached(id) })
	if err != nil {
		t.Fatal(err)
	}
	if asked != "anachryon:.dross-runs/r-1" {
		t.Errorf("fetched %q, want the recorded host's run dir", asked)
	}
	logs := verifyLogs(t, dir, "verify-"+id+"-gremlins-*.log")
	if len(logs) != 1 || !strings.Contains(readLog(t, logs[0]), "HOST-LOG-MARK") {
		t.Fatalf("the host log did not land in a fresh verify log: %v", logs)
	}
	if !strings.Contains(out, filepath.Base(logs[0])) {
		t.Errorf("the collected log's path was not printed:\n%s", out)
	}
}

func TestCollectLogFetchFailureIsANote(t *testing.T) {
	const id = "collect-log-gone"
	dir := collectRepo(t, id)
	detachFetchLog = func(remote.Target, string, string) error {
		return fmt.Errorf("ssh: connect to host anachryon: %w", remote.ErrTransport)
	}
	var err error
	out := captureStdout(t, func() { err = collectDetached(id) })
	if err != nil {
		t.Fatalf("a failed log fetch failed the collect: %v", err)
	}
	if !strings.Contains(out, "log unavailable") {
		t.Errorf("the failed fetch was not noted:\n%s", out)
	}
	testsPath, verifyPath := verify.FilePaths(filepath.Join(dir, RootDirName), id)
	for _, p := range []string{testsPath, verifyPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was not written: %v", filepath.Base(p), err)
		}
	}
}

func TestVerifyExitCodesWithLogs(t *testing.T) {
	t.Run("held host", func(t *testing.T) {
		lockVerifyRepo(t, "01-busy-logs")
		useAdapters(t, &busyAdapter{holder: remote.Holder{Project: "x"}})
		_, _, err := runVerify(t, "01-busy-logs", "--no-wait")
		if got := exitCodeOf(err); got != exitVerifyHostBusy {
			t.Fatalf("exit %d, want %d: %v", got, exitVerifyHostBusy, err)
		}
	})
	t.Run("unwritable logs", func(t *testing.T) {
		withVerifyTTY(t, false)
		verdictOf := func(t *testing.T, id string, breakLogs bool) (string, error) {
			dir := lockVerifyRepo(t, id)
			if breakLogs {
				mustWrite(t, filepath.Join(dir, ".dross", "logs"), "not a directory\n")
			}
			useAdapters(t, &streamAdapter{name: "gremlins", ext: ".go"})
			_, _, err := runVerify(t, id)
			_, vp := verify.FilePaths(filepath.Join(dir, RootDirName), id)
			v, lerr := verify.LoadVerify(vp)
			if lerr != nil {
				t.Fatal(lerr)
			}
			return v.Verify.Verdict + "/" + v.Summary.MutationStatus, err
		}
		want, wantErr := verdictOf(t, "01-logs-ok", false)
		got, gotErr := verdictOf(t, "01-logs-broken", true)
		if got != want || exitCodeOf(gotErr) != exitCodeOf(wantErr) || (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("an unwritable .dross/logs moved the verdict: %s (%v), want %s (%v)", got, gotErr, want, wantErr)
		}
	})
}

