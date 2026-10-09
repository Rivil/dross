package mutation

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// survivedLines is 300 lines of stryker-shaped per-mutant output.
func survivedLines() string {
	var b strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "[Survived] ConditionalExpression src/a.ts:%d:5 mutant-%03d\n", i+1, i)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func countSurvived(s string) int { return strings.Count(s, "[Survived] ConditionalExpression") }

// noisyStrykerNetWithReport prints out and drops the stryker.net report
// fixture where findReport looks, exiting code.
func noisyStrykerNetWithReport(t *testing.T, out string, code int) {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("testdata", "strykernet_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := filepath.Join(t.TempDir(), "tool-output.txt")
	if err := os.WriteFile(text, []byte(out+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := strykerNetBuildCmd
	strykerNetBuildCmd = func(n *StrykerNet, _ []string) *exec.Cmd {
		dir := filepath.Join(n.outputDir(), "reports")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mutation-report.json"), payload, 0o644); err != nil {
			t.Fatal(err)
		}
		return exec.Command("sh", "-c", fmt.Sprintf("cat %s; exit %d", text, code))
	}
	t.Cleanup(func() { strykerNetBuildCmd = orig })
}

func TestStrykerStreamToSink(t *testing.T) {
	run := func(t *testing.T, stream bool) (string, *Report, string) {
		s := &Stryker{ProjectRoot: t.TempDir()}
		defer noisyStryker(t, s, survivedLines(), 0, func(p string) { writeReport(t, p, "src/a.ts") })()
		var l = streamLog(t)
		if stream {
			s.SetStream(l)
		}
		out, rep, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
		if err != nil {
			t.Fatal(err)
		}
		logged := ""
		if stream {
			logged = logText(t, l)
		}
		return out, rep, logged
	}
	_, want, _ := run(t, false)
	stderr, got, logged := run(t, true)
	if n := countSurvived(stderr); n != 0 {
		t.Errorf("%d [Survived] lines reached stderr with a stream set", n)
	}
	if n := countSurvived(logged); n != 300 {
		t.Errorf("the run log holds %d of the 300 [Survived] lines", n)
	}
	if got.Killed != want.Killed || got.Survived != want.Survived || got.Score != want.Score {
		t.Errorf("the report changed with a stream set: got %+v, want %+v", got, want)
	}
}

func TestStrykerNetStreamToSink(t *testing.T) {
	noisyStrykerNetWithReport(t, survivedLines(), 0)
	plain := &StrykerNet{ProjectRoot: t.TempDir(), OutputDir: "StrykerOutput"}
	_, want, err := captureStderr(t, func() (*Report, error) { return plain.Run([]string{"src/A.cs"}) })
	if err != nil {
		t.Fatal(err)
	}
	l := streamLog(t)
	s := &StrykerNet{ProjectRoot: t.TempDir(), OutputDir: "StrykerOutput"}
	s.SetStream(l)
	stderr, got, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/A.cs"}) })
	if err != nil {
		t.Fatal(err)
	}
	if n := countSurvived(stderr); n != 0 {
		t.Errorf("%d [Survived] lines reached stderr with a stream set", n)
	}
	if n := countSurvived(logText(t, l)); n != 300 {
		t.Errorf("the run log holds %d of the 300 [Survived] lines", n)
	}
	if got.Killed != want.Killed || got.Survived != want.Survived {
		t.Errorf("the report changed with a stream set: got %+v, want %+v", got, want)
	}
}

func TestStrykerFailureHeadStaysOnStderr(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	defer noisyStryker(t, s, survivedLines(), 3, nil)()
	s.SetStream(streamLog(t))
	stderr, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
	if err == nil {
		t.Fatal("a reportless non-zero exit returned nil error")
	}
	if n := countSurvived(stderr); n == 0 || n > strykerHeadLines {
		t.Errorf("stderr holds %d [Survived] lines, want the bounded head (1..%d)", n, strykerHeadLines)
	}
}

func TestStrykerNilStreamKeepsStderr(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	defer noisyStryker(t, s, survivedLines(), 0, func(p string) { writeReport(t, p, "src/a.ts") })()
	s.SetStream(streamLog(t))
	s.SetStream(nil)
	stderr, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
	if err != nil {
		t.Fatal(err)
	}
	if n := countSurvived(stderr); n != 300 {
		t.Errorf("with no stream, stderr holds %d of the 300 lines — the default moved", n)
	}
}

func TestStrykerFailureRecordsLogPointer(t *testing.T) {
	for _, stream := range []bool{false, true} {
		want := StderrPointer
		if stream {
			want = LogPointer
		}
		t.Run(fmt.Sprintf("stryker stream=%v", stream), func(t *testing.T) {
			s := &Stryker{ProjectRoot: t.TempDir()}
			defer noisyStryker(t, s, "INFO Stryker Starting", 3, nil)()
			if stream {
				s.SetStream(streamLog(t))
			}
			_, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
			var rec *toolFailure
			if !errors.As(err, &rec) || !strings.Contains(err.Error(), want) {
				t.Fatalf("the record does not say %q: %v", want, err)
			}
		})
		t.Run(fmt.Sprintf("stryker-net stream=%v", stream), func(t *testing.T) {
			s := &StrykerNet{ProjectRoot: t.TempDir(), OutputDir: "StrykerOutput"}
			defer noisyStrykerNet(t, "dotnet stryker: starting", 5)()
			if stream {
				s.SetStream(streamLog(t))
			}
			_, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/A.cs"}) })
			var rec *toolFailure
			if !errors.As(err, &rec) || !strings.Contains(err.Error(), want) {
				t.Fatalf("the record does not say %q: %v", want, err)
			}
		})
	}
}
