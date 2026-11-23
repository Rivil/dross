//go:build !windows

package precond

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/testlane"
)

// `sh` lines stand in for a plan's precondition commands: same spawn path, no
// toolchain or network needed. Unix-only because the process-group kill they
// prove is.

func f(v float64) *float64 { return &v }

// render is Render captured as a string.
func render(v Verdict) string {
	var b strings.Builder
	v.Render(&b)
	return b.String()
}

func evalOne(t *testing.T, dir string, p phase.Precondition) Result {
	t.Helper()
	v := Evaluate(context.Background(), dir, []phase.Precondition{p})
	if len(v.Checked) != 1 {
		t.Fatalf("Checked = %d results, want 1", len(v.Checked))
	}
	return v.Checked[0]
}

// shQuote single-quotes s for sh.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestFailureRendersPreconditionRunWhyExitAndTail(t *testing.T) {
	pres := []phase.Precondition{
		{Run: "true"},
		{Run: "echo boom; exit 3", Why: "the build must be green"},
	}
	v := Evaluate(context.Background(), t.TempDir(), pres)
	if v.OK() {
		t.Fatal("a precondition that exited 3 passed")
	}
	out := render(v)
	for _, want := range []string{"precondition 2", "echo boom; exit 3", "the build must be green", "exit 3", "boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("Render() lacks %q:\n%s", want, out)
		}
	}
}

func TestTailIsTheLastTwentyLines(t *testing.T) {
	r := evalOne(t, t.TempDir(), phase.Precondition{Run: `i=1; while [ $i -le 50 ]; do echo "line $i"; i=$((i+1)); done; exit 1`})
	if len(r.Tail) != 20 || r.Tail[0] != "line 31" || r.Tail[19] != "line 50" {
		t.Fatalf("Tail = %q, want exactly lines 31–50", r.Tail)
	}
}

func TestOversizedStdoutIsUnparseableAndBounded(t *testing.T) {
	r := evalOne(t, t.TempDir(), phase.Precondition{
		Run: `head -c 10485760 /dev/zero | tr '\0' 'x'`, Max: f(5),
	})
	if r.Passed || !strings.Contains(r.Reason, "unparseable") {
		t.Fatalf("10 MiB of stdout gave passed=%v reason %q, want unparseable", r.Passed, r.Reason)
	}
	if len(r.Reason) > 1024 {
		t.Errorf("reason is %d bytes, want the quote capped", len(r.Reason))
	}
	tail := strings.Join(r.Tail, "\n")
	if len(tail) > tailBytes {
		t.Errorf("tail is %d bytes, want at most %d", len(tail), tailBytes)
	}
}

func TestWritersStayBounded(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 10<<20)
	c := &capWriter{max: stdoutCap}
	if n, err := c.Write(big); n != len(big) || err != nil {
		t.Fatalf("capWriter.Write = %d, %v; want a full write", n, err)
	}
	if len(c.buf) != stdoutCap || !c.over {
		t.Errorf("capWriter kept %d bytes (over=%v), want %d and over", len(c.buf), c.over, stdoutCap)
	}
	w := &tailWriter{}
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(w, "%04d %s\n", i, strings.Repeat("y", 100))
	}
	if _, err := w.Write(big); err != nil {
		t.Fatal(err)
	}
	if len(w.buf) > tailBytes {
		t.Errorf("tailWriter kept %d bytes, want at most %d", len(w.buf), tailBytes)
	}
}

func TestBoundsAreInclusive(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		run      string
		max, min *float64
		pass     bool
		reason   string
	}{
		{"echo 5", f(5), nil, true, ""},
		{"echo 5.0001", f(5), nil, false, "measured 5.0001, bound max 5"},
		{"echo 80", nil, f(80), true, ""},
		{"echo 79.9", nil, f(80), false, "measured 79.9, bound min 80"},
		{"echo 0.3", f(0.3), nil, true, ""},
		{"echo 4", f(5), f(4), true, ""},
	} {
		r := evalOne(t, dir, phase.Precondition{Run: c.run, Max: c.max, Min: c.min})
		if r.Passed != c.pass {
			t.Errorf("%q: passed = %v (%s), want %v", c.run, r.Passed, r.Reason, c.pass)
		}
		if c.reason != "" && r.Reason != c.reason {
			t.Errorf("%q: reason = %q, want %q", c.run, r.Reason, c.reason)
		}
	}
}

func TestMeasurementsNeverCoerce(t *testing.T) {
	dir := t.TempDir()
	for _, out := range []string{"", "12 ms", "1\n2", "NaN", "Inf", "+Inf", "infinity", "0x1p-2", "1_000", "1,234", "3%", "warming up\n3"} {
		for _, b := range []phase.Precondition{{Max: f(5)}, {Min: f(-1)}} {
			b.Run = "printf '%s' " + shQuote(out)
			r := evalOne(t, dir, b)
			if r.Passed {
				t.Errorf("stdout %q passed a bounded precondition", out)
				continue
			}
			if !strings.Contains(r.Reason, "unparseable") || !strings.Contains(r.Reason, strconv.Quote(out)) {
				t.Errorf("stdout %q: reason %q, want unparseable quoting %s", out, r.Reason, strconv.Quote(out))
			}
		}
	}
}

func TestExitStatusComesBeforeTheMeasurement(t *testing.T) {
	dir := t.TempDir()
	r := evalOne(t, dir, phase.Precondition{Run: "echo 3; exit 1", Max: f(5)})
	if r.Passed || r.Reason != "exit 1" || r.Measured != nil {
		t.Errorf("measurement 3 with exit 1: passed=%v reason=%q measured=%v, want a fail on \"exit 1\"", r.Passed, r.Reason, r.Measured)
	}
	if r := evalOne(t, dir, phase.Precondition{Run: "echo abc"}); !r.Passed {
		t.Errorf("an unbounded exit-0 precondition failed: %s", r.Reason)
	}
}

func TestStderrIsTailedNotParsed(t *testing.T) {
	r := evalOne(t, t.TempDir(), phase.Precondition{Run: "echo 3; echo junk >&2", Max: f(5)})
	if !r.Passed {
		t.Fatalf("stderr reached the parser: %s", r.Reason)
	}
	tail := strings.Join(r.Tail, "\n")
	if !strings.Contains(tail, "3") || !strings.Contains(tail, "junk") {
		t.Errorf("tail %q lacks stdout or stderr", tail)
	}
}

func TestTimeoutKillsAndSaysSo(t *testing.T) {
	start := time.Now()
	v := Evaluate(context.Background(), t.TempDir(), []phase.Precondition{{Run: "sleep 30", Timeout: "200ms"}})
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond+5*time.Second {
		t.Errorf("Evaluate took %s, past the timeout plus WaitDelay", elapsed)
	}
	if v.OK() {
		t.Fatal("a precondition past its timeout passed")
	}
	out := render(v)
	if !strings.Contains(out, "timed out after 200ms") || strings.Contains(out, "exit") {
		t.Errorf("Render() = %q, want \"timed out after 200ms\" and no exit status", out)
	}
}

func TestTimeoutKillsTheWholeGroup(t *testing.T) {
	dir := t.TempDir()
	v := Evaluate(context.Background(), dir, []phase.Precondition{{Run: "sleep 30 & echo $! > pid; wait", Timeout: "200ms"}})
	if v.OK() {
		t.Fatal("a precondition past its timeout passed")
	}
	b, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	// The killed grandchild is reparented and reaped; give that a moment.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL) // don't leak it past the test
			t.Fatalf("grandchild %d still alive after the timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSpawnFailureIsNeverExitZero(t *testing.T) {
	r := evalOne(t, filepath.Join(t.TempDir(), "missing"), phase.Precondition{Run: "true"})
	if r.Passed || !strings.HasPrefix(r.Reason, "did not run") {
		t.Errorf("a spawn with no exit code gave passed=%v reason %q, want a \"did not run\" fail", r.Passed, r.Reason)
	}
	r = evalOne(t, t.TempDir(), phase.Precondition{Run: "-i"})
	if r.Passed || !strings.Contains(r.Reason, "task.preconditions.run") {
		t.Errorf("a leading-dash run gave passed=%v reason %q, want a fence refusal naming the field", r.Passed, r.Reason)
	}
}

func TestInvalidTimeoutNeverRuns(t *testing.T) {
	dir := t.TempDir()
	r := evalOne(t, dir, phase.Precondition{Run: "touch ran", Timeout: "soon"})
	if r.Passed {
		t.Fatal("a malformed timeout passed")
	}
	if _, err := os.Stat(filepath.Join(dir, "ran")); err == nil {
		t.Error("a precondition with a malformed timeout ran")
	}
}

func TestRunsAtRepoDirNotTheProcessCwd(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "marker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if r := evalOne(t, repo, phase.Precondition{Run: "test -f marker"}); !r.Passed {
		t.Errorf("`test -f marker` failed at repoDir: %s", r.Reason)
	}
}

func TestEvaluationStopsAtTheFirstFailure(t *testing.T) {
	dir := t.TempDir()
	v := Evaluate(context.Background(), dir, []phase.Precondition{{Run: "exit 1"}, {Run: "touch ran"}})
	if v.OK() || len(v.Checked) != 1 || v.Total != 2 {
		t.Fatalf("verdict = %+v, want one failed result of two", v)
	}
	if _, err := os.Stat(filepath.Join(dir, "ran")); err == nil {
		t.Error("a precondition after the failure ran")
	}
}

func TestPassRendersEachPrecondition(t *testing.T) {
	v := Evaluate(context.Background(), t.TempDir(), []phase.Precondition{{Run: "true"}, {Run: "echo 3", Max: f(5)}})
	if !v.OK() {
		t.Fatalf("passing preconditions failed: %s", render(v))
	}
	out := render(v)
	for _, want := range []string{"passed (2 of 2)", "1: true — exit 0", "2: echo 3 — measured 3 (max 5)"} {
		if !strings.Contains(out, want) {
			t.Errorf("Render() lacks %q:\n%s", want, out)
		}
	}
	if got := render(Verdict{}); got != "no preconditions\n" {
		t.Errorf("empty verdict renders %q", got)
	}
}

// TestRunLocalKeepsItsFenceLabel: extracting RunLine must not relabel the
// suite's own fence, nor put the suite in a group of its own.
func TestRunLocalKeepsItsFenceLabel(t *testing.T) {
	var out bytes.Buffer
	err := testlane.RunLocal(context.Background(), t.TempDir(), "-x", &out, &out)
	if err == nil || !strings.Contains(err.Error(), "runtime.test_command") {
		t.Errorf("RunLocal(-x) = %v, want a refusal naming runtime.test_command", err)
	}
	// ps prints the child's pgid; RunLocal's child shares ours.
	out.Reset()
	if err := testlane.RunLocal(context.Background(), t.TempDir(), "ps -o pgid= -p $$", &out, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(out.String()), strconv.Itoa(syscall.Getpgrp()); got != want {
		t.Errorf("RunLocal child pgid = %s, want dross's own %s", got, want)
	}
}
