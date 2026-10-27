package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// fakeGremlinsOnPath puts a `gremlins` on PATH that prints 300 per-mutant
// lines and writes one LIVED survivor (internal/y.go:4) to its --output path.
func fakeGremlinsOnPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	report := gremlinsReportWith("y.go", mutantJSON(4, "CONDITIONALS_NEGATION", "LIVED"))
	script := "#!/bin/sh\nout=''\nprev=''\nfor a in \"$@\"; do\n  if [ \"$prev\" = \"--output\" ]; then out=\"$a\"; fi\n  prev=\"$a\"\ndone\n" +
		"i=1\nwhile [ $i -le 300 ]; do\n  echo \"      LIVED CONDITIONALS_NEGATION at internal/y.go:4:2 fake-mutant-$i\"\n  i=$((i+1))\ndone\n" +
		"mkdir -p \"$(dirname \"$out\")\"\nprintf '%s' '" + report + "' > \"$out\"\n"
	if err := os.WriteFile(filepath.Join(dir, "gremlins"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func countFakeMutants(s string) int { return strings.Count(s, "fake-mutant-") }

// drainLogs lists the drain fixture's verify run logs.
func drainLogs(t *testing.T, repo string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repo, ".dross", "logs", "verify-*.log"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func withDrainTTY(t *testing.T, tty bool) {
	t.Helper()
	orig := drainStdoutIsTTY
	t.Cleanup(func() { drainStdoutIsTTY = orig })
	drainStdoutIsTTY = func() bool { return tty }
}

// runDrain runs `survivor drain` and returns stdout, stderr and the error.
func runDrain(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut string
	var err error
	errOut = captureStderr(t, func() {
		err = runCmdCapturing(t, &out, Survivor(), append([]string{"drain"}, args...)...)
	})
	return out, errOut, err
}

func TestDrainLogsGremlinsStream(t *testing.T) {
	for _, tc := range []struct {
		name, prefix string
		args         []string
	}{
		{"no phase", "verify-drain-gremlins-", nil},
		{"--phase", "verify-alpha-gremlins-", []string{"--phase", "alpha"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := drainFixture(t, []string{"./internal"})
			fakeGremlinsOnPath(t)
			withDrainTTY(t, false)
			out, errOut, _ := runDrain(t, tc.args...)
			if n := countFakeMutants(out + errOut); n != 0 {
				t.Errorf("%d per-mutant lines reached stdout or stderr", n)
			}
			logs := drainLogs(t, dir)
			if len(logs) != 1 || !strings.HasPrefix(filepath.Base(logs[0]), tc.prefix) {
				t.Fatalf("drain logs %v, want one %s*.log", logs, tc.prefix)
			}
			b, err := os.ReadFile(logs[0])
			if err != nil {
				t.Fatal(err)
			}
			if n := countFakeMutants(string(b)); n != 300 {
				t.Errorf("the drain log holds %d of the 300 per-mutant lines", n)
			}
			if !strings.Contains(out, "drain log: "+logs[0]) {
				t.Errorf("the drain did not print its log's absolute path:\n%s", out)
			}
		})
	}
}

func TestDrainTTYStillStreams(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	fakeGremlinsOnPath(t)
	withDrainTTY(t, true)
	_, errOut, _ := runDrain(t)
	if n := countFakeMutants(errOut); n != 300 {
		t.Errorf("on a terminal, stderr holds %d of the 300 lines", n)
	}
	logs := drainLogs(t, dir)
	if len(logs) != 1 {
		t.Fatalf("drain logs %v, want one", logs)
	}
	b, _ := os.ReadFile(logs[0])
	if n := countFakeMutants(string(b)); n != 300 {
		t.Errorf("the drain log holds %d of the 300 lines", n)
	}
}

// verdict is a drain run's outcome without its log line.
func verdict(out string, err error) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "drain log: ") {
			keep = append(keep, l)
		}
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return strings.Join(keep, "\n") + "\n--\n" + msg
}

func TestDrainVerdictUnchangedWithLog(t *testing.T) {
	withDrainTTY(t, false)
	dir := drainFixture(t, []string{"./internal"})
	fakeGremlinsOnPath(t)
	out, _, err := runDrain(t)
	if err == nil || !strings.Contains(out, "internal/y.go:4 (CONDITIONALS_NEGATION)") {
		t.Fatalf("the drain did not report the fake survivor (err %v):\n%s", err, out)
	}
	want, wantCode := verdict(out, err), ExitCode(err)

	// The same run with nowhere to keep the log: the stream falls back to
	// stderr with one warning, and the verdict does not move.
	if err := os.RemoveAll(filepath.Join(dir, ".dross", "logs")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(mutation.GremlinsReportPath(dir, "./internal")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "logs"), "not a directory\n")
	out2, errOut2, err2 := runDrain(t)
	if got := verdict(out2, err2); got != want || ExitCode(err2) != wantCode {
		t.Errorf("the verdict moved with the log unavailable:\n got %s\nwant %s", got, want)
	}
	if n := strings.Count(errOut2, "warning: dross could not keep this run's log"); n != 1 {
		t.Errorf("%d log warnings, want exactly one:\n%s", n, errOut2)
	}
	if n := countFakeMutants(errOut2); n != 300 {
		t.Errorf("with no log the stream did not fall back to stderr (%d lines)", n)
	}
}

func TestDrainReportModeNoMutationLog(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	fakeGremlinsOnPath(t)
	withDrainTTY(t, false)
	writeRawReport(t, dir, "./internal", gremlinsReportWith("y.go", mutantJSON(4, "CONDITIONALS_NEGATION", "LIVED")))
	runDrain(t, "--report", mutation.GremlinsReportPath(dir, "./internal"))
	if logs := drainLogs(t, dir); len(logs) != 0 {
		t.Fatalf("--report mode opened a mutation log: %v", logs)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross", "logs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("--report mode created .dross/logs (stat err = %v)", err)
	}
}
