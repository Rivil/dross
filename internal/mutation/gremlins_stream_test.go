package mutation

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/runlog"
)

// mutantLines is 300 lines of gremlins-shaped per-mutant output, each unique.
func mutantLines() string {
	var b strings.Builder
	for i := 0; i < 300; i++ {
		status := "KILLED"
		if i%3 == 0 {
			status = "LIVED"
		}
		fmt.Fprintf(&b, "%11s CONDITIONALS_NEGATION at internal/cmd/verify.go:%d:2 mutant-%03d\n", status, i+1, i)
	}
	return b.String()
}

// streamLog is a run log in its own temp root, echo off.
func streamLog(t *testing.T) *runlog.Log {
	t.Helper()
	return runlog.Open(t.TempDir(), runlog.Name{Kind: runlog.KindVerify, Phase: "p", Leg: "gremlins"}, runlog.Options{Terminal: runlog.Stderr})
}

func logText(t *testing.T, l *runlog.Log) string {
	t.Helper()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if l.Path() == "" {
		t.Fatalf("the run log was never written (err %v)", l.Err())
	}
	b, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// noisyGremlins makes the local gremlins print lines and drop payload at its
// --output path.
func noisyGremlins(t *testing.T, lines string, payload []byte) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "tool-output.txt")
	if err := os.WriteFile(out, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := gremlinsBuildCmd
	gremlinsBuildCmd = func(g *Gremlins, args []string) *exec.Cmd {
		for i, a := range args {
			if a == "--output" && i+1 < len(args) {
				if err := os.WriteFile(filepath.Join(g.ProjectRoot, args[i+1]), payload, 0o644); err != nil {
					t.Fatalf("write fake gremlins report: %v", err)
				}
			}
		}
		return exec.Command("cat", out)
	}
	t.Cleanup(func() { gremlinsBuildCmd = orig })
}

func countMutantLines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "CONDITIONALS_NEGATION at internal/cmd/verify.go") {
			n++
		}
	}
	return n
}

func TestGremlinsStreamToSink(t *testing.T) {
	lines := mutantLines()
	noisyGremlins(t, lines, []byte(fixtureGremlins))

	plain := &Gremlins{ProjectRoot: t.TempDir()}
	_, want, err := captureStderr(t, func() (*Report, error) { return plain.Run([]string{"internal/cmd/verify.go"}) })
	if err != nil {
		t.Fatal(err)
	}

	l := streamLog(t)
	g := &Gremlins{ProjectRoot: t.TempDir()}
	g.SetStream(l)
	stderr, got, err := captureStderr(t, func() (*Report, error) { return g.Run([]string{"internal/cmd/verify.go"}) })
	if err != nil {
		t.Fatal(err)
	}
	if n := countMutantLines(stderr); n != 0 {
		t.Errorf("%d per-mutant lines reached stderr with a stream set:\n%s", n, stderr)
	}
	logged := logText(t, l)
	if n := countMutantLines(logged); n != 300 {
		t.Errorf("the run log holds %d of the 300 per-mutant lines", n)
	}
	if !strings.Contains(logged, "gremlins: ") {
		t.Error("the per-package invocation echo did not go to the stream")
	}
	if got.Killed != want.Killed || got.Survived != want.Survived || got.NotCovered != want.NotCovered || got.Timeout != want.Timeout {
		t.Errorf("the report changed with a stream set: got %+v, want %+v", got, want)
	}
}

func TestGremlinsRemoteArmStreamToSink(t *testing.T) {
	failLocalSpawns(t)
	noisyRemoteTool(t, strings.TrimSuffix(mutantLines(), "\n"), 0)

	l := streamLog(t)
	g := remoteGremlins(t.TempDir())
	g.SetStream(l)
	stderr, _, err := captureStderr(t, func() (*Report, error) { return g.Run([]string{"internal/cmd/verify.go"}) })
	if err != nil {
		t.Fatal(err)
	}
	if n := countMutantLines(stderr); n != 0 {
		t.Errorf("%d remote per-mutant lines reached stderr with a stream set", n)
	}
	if n := countMutantLines(logText(t, l)); n != 300 {
		t.Errorf("the run log holds %d of the 300 remote per-mutant lines", n)
	}
	if !strings.Contains(stderr, "gremlins: skipped") {
		t.Errorf("the skipped-package line left stderr:\n%s", stderr)
	}
}

func TestGremlinsFailureHeadStaysOnStderr(t *testing.T) {
	failLocalSpawns(t)
	noisyRemoteTool(t, strings.TrimSuffix(mutantLines(), "\n"), 2)

	l := streamLog(t)
	g := remoteGremlins(t.TempDir())
	g.SetStream(l)
	stderr, _, err := captureStderr(t, func() (*Report, error) { return g.Run([]string{"internal/cmd/verify.go"}) })
	var rec *toolFailure
	if err == nil || !errors.As(err, &rec) {
		t.Fatalf("a reportless exit 2 did not return a tool-failure record: %v", err)
	}
	head := countMutantLines(stderr)
	if head == 0 || head > strykerHeadLines {
		t.Errorf("stderr holds %d per-mutant lines, want the bounded head (1..%d) — the head is what diagnoses the failure", head, strykerHeadLines)
	}
	if !strings.Contains(err.Error(), LogPointer) {
		t.Errorf("the record does not point at the run log: %v", err)
	}
	if n := countMutantLines(logText(t, l)); n != 300 {
		t.Errorf("the run log holds %d of the 300 lines", n)
	}
}

func TestGremlinsNilStreamKeepsStderr(t *testing.T) {
	noisyGremlins(t, mutantLines(), []byte(fixtureGremlins))
	g := &Gremlins{ProjectRoot: t.TempDir()}
	g.SetStream(streamLog(t))
	g.SetStream(nil)
	stderr, _, err := captureStderr(t, func() (*Report, error) { return g.Run([]string{"internal/cmd/verify.go"}) })
	if err != nil {
		t.Fatal(err)
	}
	if n := countMutantLines(stderr); n != 300 {
		t.Errorf("with no stream, stderr holds %d of the 300 lines — the default moved", n)
	}
}
