package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/pflag"
)

// testLogs lists the run logs under the fixture repo's .dross/logs/.
func testLogs(t *testing.T) []string {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "logs", "test-*.log"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// onlyLog is the one run log the fixture holds, read.
func onlyLog(t *testing.T) string {
	t.Helper()
	paths := testLogs(t)
	if len(paths) != 1 {
		t.Fatalf("%d run logs, want exactly one: %v", len(paths), paths)
	}
	b, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// markSpawns makes every local spawn write OUT-MARK to its stdout and
// ERR-MARK to its stderr, and records whether it was handed one writer.
type markSpawns struct {
	mu   sync.Mutex
	same []bool
}

func (m *markSpawns) note(stdout, stderr io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.same = append(m.same, stdout == stderr)
}

func installMarkSpawns(t *testing.T) *markSpawns {
	t.Helper()
	m := &markSpawns{}
	origL, origR := spawnLocal, spawnRemote
	t.Cleanup(func() { spawnLocal, spawnRemote = origL, origR })
	spawnLocal = func(_, _ string, stdout, stderr io.Writer) error {
		m.note(stdout, stderr)
		io.WriteString(stdout, "OUT-MARK\n")
		io.WriteString(stderr, "ERR-MARK\n")
		return nil
	}
	spawnRemote = func(argv []string, _ string, stdout, stderr io.Writer) error {
		m.note(stdout, stderr)
		if argv[0] == "rsync" {
			io.WriteString(stdout, "RSYNC-MARK\n")
			return nil
		}
		io.WriteString(stdout, "OUT-MARK\n")
		io.WriteString(stderr, "ERR-MARK\n")
		return nil
	}
	return m
}

// tempStdout points os.Stdout at a file for fn, so a spawn seam can read what
// was printed before it ran.
func tempStdout(t *testing.T, fn func(path string)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdout")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = f
	fn(path)
	os.Stdout = orig
	f.Close()
	b, _ := os.ReadFile(path)
	return string(b)
}

func TestLogPathBeforeSpawn(t *testing.T) {
	testFixture(t, "go test ./...")
	trustFixture(t)
	var seen string
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	tempStdout(t, func(path string) {
		spawnLocal = func(_, _ string, _, _ io.Writer) error {
			b, _ := os.ReadFile(path)
			seen = string(b)
			return nil
		}
		if err := runCmd(t, Test()); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(seen, "log: /") || !strings.Contains(seen, "/.dross/logs/test-") {
		t.Fatalf("stdout before the spawn = %q, want the log's absolute path already printed", seen)
	}
}

func TestEveryRunWritesLog(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T)
		args  []string
		rsync bool
	}{
		{"bare", func(t *testing.T) { testFixture(t, "go test ./..."); trustFixture(t) }, nil, false},
		{"selector", func(t *testing.T) { testFixture(t, "go test ./..."); trustFixture(t) }, []string{"./internal/..."}, false},
		{"files", func(t *testing.T) { filesFixture(t, goAndDocsLanes); grantAllLanes(t) }, []string{"--files", "internal/cmd/test.go"}, false},
		{"lanes-only bare", func(t *testing.T) {
			dir := laneFixture(t)
			appendLanes(t, dir, goAndDocsLanes)
			grantAllLanes(t)
			installLaneLookPath(t)
		}, nil, false},
		{"remote", func(t *testing.T) { grantedTestFixture(t, "go test ./...") }, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			installMarkSpawns(t)
			out := captureStdout(t, func() {
				if err := runCmd(t, Test(), tc.args...); err != nil {
					t.Fatalf("dross test: %v", err)
				}
			})
			log := onlyLog(t)
			for _, want := range []string{"OUT-MARK", "ERR-MARK"} {
				if !strings.Contains(log, want) {
					t.Errorf("the run log lacks %s:\n%s", want, log)
				}
			}
			if tc.rsync && !strings.Contains(log, "RSYNC-MARK") {
				t.Errorf("the remote run's log lacks the rsync segment:\n%s", log)
			}
			if n := strings.Count(out, "log: "); n != 2 {
				t.Errorf("stdout names the log %d times, want before the first spawn and at the end:\n%s", n, out)
			}
		})
	}
}

func TestRefusalWritesNoLog(t *testing.T) {
	testFixture(t, "go test ./...")
	installMarkSpawns(t)
	if err := runCmd(t, Test()); err == nil {
		t.Fatal("an untrusted run did not refuse")
	}
	root, _ := FindRoot()
	if _, err := os.Stat(filepath.Join(root, "logs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused run created .dross/logs (stat err = %v)", err)
	}
}

func TestOneWriterBothStreams(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	m := installMarkSpawns(t)
	captureStdout(t, func() {
		if err := runCmd(t, Test()); err != nil {
			t.Fatal(err)
		}
		if err := runCmd(t, Test(), "--local"); err != nil {
			t.Fatal(err)
		}
	})
	if len(m.same) < 3 {
		t.Fatalf("%d spawns seen, want the rsync, the ssh and the local run", len(m.same))
	}
	for i, same := range m.same {
		if !same {
			t.Errorf("spawn %d got different stdout and stderr writers", i)
		}
	}
}

func TestTTYStreamsNonTTYSummarises(t *testing.T) {
	green := "ok  \texample.com/x\t0.01s\nTTY-SENTINEL\n"
	run := func(t *testing.T, tty bool) (string, string) {
		testFixture(t, "go test ./...")
		trustFixture(t)
		orig, origTTY := spawnLocal, testStdoutIsTTY
		t.Cleanup(func() { spawnLocal, testStdoutIsTTY = orig, origTTY })
		testStdoutIsTTY = func() bool { return tty }
		spawnLocal = func(_, _ string, stdout, _ io.Writer) error {
			io.WriteString(stdout, green)
			return nil
		}
		out := captureStdout(t, func() {
			if err := runCmd(t, Test()); err != nil {
				t.Fatal(err)
			}
		})
		return out, onlyLog(t)
	}
	t.Run("not a terminal", func(t *testing.T) {
		out, log := run(t, false)
		if strings.Contains(out, "TTY-SENTINEL") || !strings.Contains(log, "TTY-SENTINEL") {
			t.Fatalf("non-TTY: the sentinel must be in the log and not on stdout:\nstdout:\n%s", out)
		}
		if !strings.Contains(out, "PASS") {
			t.Fatalf("non-TTY printed no summary:\n%s", out)
		}
	})
	t.Run("terminal", func(t *testing.T) {
		out, log := run(t, true)
		if !strings.Contains(out, "TTY-SENTINEL") || !strings.Contains(log, "TTY-SENTINEL") {
			t.Fatalf("TTY: the sentinel must be on stdout and in the log:\nstdout:\n%s", out)
		}
		if strings.Contains(out, "PASS ·") || strings.Contains(out, "last ") {
			t.Fatalf("TTY printed a summary block:\n%s", out)
		}
	})
}

func TestLogUnavailableStreams(t *testing.T) {
	testFixture(t, "go test ./...")
	trustFixture(t)
	root, _ := FindRoot()
	mustWrite(t, filepath.Join(root, "logs"), "not a directory\n")
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, _ string, stdout, _ io.Writer) error {
		io.WriteString(stdout, "UNAVAILABLE-SENTINEL\n")
		return errors.New("exit status 1")
	}
	var err error
	var stderr string
	out := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = runCmd(t, Test()) })
	})
	if !strings.Contains(out, "UNAVAILABLE-SENTINEL") {
		t.Fatalf("with no log the output did not stream to stdout:\n%s", out)
	}
	if n := strings.Count(out+stderr, "warning: dross could not keep this run's log"); n != 1 {
		t.Fatalf("%d log warnings, want exactly one:\nstdout:\n%s\nstderr:\n%s", n, out, stderr)
	}
	if ExitCode(err) != exitSuiteFailed {
		t.Fatalf("exit code %d, want the suite's own %d", ExitCode(err), exitSuiteFailed)
	}
}

func TestNoStreamingFlag(t *testing.T) {
	var got []string
	Test().Flags().VisitAll(func(f *pflag.Flag) { got = append(got, f.Name) })
	sort.Strings(got)
	if strings.Join(got, ",") != "files,local,wait" {
		t.Fatalf("dross test defines flags %v, want exactly files, local and wait", got)
	}
}
