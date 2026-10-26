package runsummary

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

const logPath = "/repo/.dross/logs/test-20261009T120000.000000000Z.log"

// fake is a toy runner: "ok <pkg>" counts a pass, "FAIL <name>" opens a
// failure and tab-indented lines after it are its output. It claims a segment
// once it has seen either.
type fake struct {
	ok      int
	fs      Failures
	cur     *Failure
	claimed bool
	fed     *[]string
}

func (f *fake) Feed(line string) {
	if f.fed != nil {
		*f.fed = append(*f.fed, line)
	}
	switch {
	case strings.HasPrefix(line, "ok "):
		f.flush()
		f.ok++
		f.claimed = true
	case strings.HasPrefix(line, "FAIL "):
		f.flush()
		f.cur = &Failure{Name: strings.TrimPrefix(line, "FAIL ")}
		f.claimed = true
	case f.cur != nil && strings.HasPrefix(line, "\t"):
		f.cur.Line(line)
	}
}

func (f *fake) flush() {
	if f.cur != nil {
		f.fs.Add(*f.cur)
		f.cur = nil
	}
}

func (f *fake) Claims() bool { return f.claimed }

func (f *fake) Result() Result {
	f.flush()
	return Result{Runner: "fake", Counts: []string{fmt.Sprintf("%d ok", f.ok)}, Failures: f.fs}
}

// withParsers swaps the registry for one test.
func withParsers(t *testing.T, ps ...func() Parser) {
	t.Helper()
	regMu.Lock()
	prev := registry
	registry = ps
	regMu.Unlock()
	t.Cleanup(func() {
		regMu.Lock()
		registry = prev
		regMu.Unlock()
	})
}

func newFake() Parser { return &fake{} }

func feed(s *Segment, text string) *Segment {
	s.Write([]byte(text))
	s.Close()
	return s
}

func segment(text string) *Segment { return feed(NewSegment(), text) }

func outLines(out string) []string { return strings.Split(strings.TrimSuffix(out, "\n"), "\n") }

func hasLine(out, want string) bool {
	for _, l := range outLines(out) {
		if l == want {
			return true
		}
	}
	return false
}

// failures writes n failures of m output lines, each line width bytes long.
func failures(n, m, width int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "FAIL fail-%03d\n", i)
		for j := 0; j < m; j++ {
			b.WriteString("\t" + strings.Repeat("x", width-1) + "\n")
		}
	}
	return b.String()
}

func TestNonZeroNeverGreen(t *testing.T) {
	withParsers(t, newFake)
	var b strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "ok pkg/%d\n", i)
	}
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{segment(b.String())}, &Budget{}, logPath, time.Second)
	if strings.Contains(out, "PASS") {
		t.Fatalf("exit 1 rendered green:\n%s", out)
	}
	if !strings.HasPrefix(out, "exit 1 ") || !hasLine(out, "ok pkg/0") || !hasLine(out, "ok pkg/11") || !hasLine(out, "log: "+logPath) {
		t.Fatalf("want the fallback (exit 1, the tail, the log path):\n%s", out)
	}
	if strings.Contains(out, "12 ok") {
		t.Fatalf("the fallback printed counts:\n%s", out)
	}
}

func TestRunDidNotHappenNeverCounts(t *testing.T) {
	withParsers(t, newFake)
	for _, exit := range []int{3, 4} {
		out := Render(Outcome{Exit: exit, Ran: false}, []*Segment{segment("ok a\nok b\nssh: connect refused\n")}, &Budget{}, logPath, time.Second)
		if !strings.HasPrefix(out, fmt.Sprintf("exit %d ", exit)) || !hasLine(out, "ssh: connect refused") || !hasLine(out, "log: "+logPath) {
			t.Fatalf("exit %d: want the exit status, the tail and the log path:\n%s", exit, out)
		}
		if strings.Contains(out, "PASS") || strings.Contains(out, "2 ok") {
			t.Fatalf("exit %d printed counts for a run that did not happen:\n%s", exit, out)
		}
	}
	// Failing output at exit 0 would otherwise print red: only Ran stops it.
	out := Render(Outcome{Exit: 0, Ran: false}, []*Segment{segment("ok a\nFAIL TestX\n\tboom\n")}, &Budget{}, logPath, time.Second)
	if !strings.HasPrefix(out, "exit 0 ") || strings.Contains(out, "FAIL (exit") || strings.Contains(out, "✗ ") {
		t.Fatalf("a run that did not happen printed failures:\n%s", out)
	}
}

func TestWriteAfterCloseDropped(t *testing.T) {
	var fed []string
	withParsers(t, func() Parser { return &fake{fed: &fed} })
	s := segment("ok a\n")
	if n, err := s.Write([]byte("ok late\n")); n != 8 || err != nil {
		t.Fatalf("Write after Close = %d, %v", n, err)
	}
	if len(fed) != 1 || len(s.Tail()) != 1 {
		t.Fatalf("a write after Close reached the parser (%q) or the tail (%q)", fed, s.Tail())
	}
}

func TestUnparseableFallback(t *testing.T) {
	withParsers(t, newFake)
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	for _, exit := range []int{2, 0} {
		out := Render(Outcome{Exit: exit, Ran: true}, []*Segment{segment(b.String())}, &Budget{}, logPath, time.Second)
		ls := outLines(out)
		if !strings.HasPrefix(ls[0], fmt.Sprintf("exit %d ", exit)) {
			t.Fatalf("exit %d: first line %q, want the exit status", exit, ls[0])
		}
		for i := 61; i <= 100; i++ {
			if !hasLine(out, fmt.Sprintf("line %d", i)) {
				t.Fatalf("exit %d: line %d missing from the tail:\n%s", exit, i, out)
			}
		}
		if hasLine(out, "line 60") {
			t.Fatalf("exit %d: the tail holds more than %d lines", exit, TailLines)
		}
		if ls[len(ls)-1] != "log: "+logPath || len(ls) != 42 {
			t.Fatalf("exit %d: want header + 40 lines + log path, got %d lines:\n%s", exit, len(ls), out)
		}
		if strings.Contains(out, "PASS") || strings.Contains(out, " ok") {
			t.Fatalf("exit %d: unparseable output printed counts:\n%s", exit, out)
		}
	}
}

func TestFailureBound(t *testing.T) {
	withParsers(t, newFake)
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{segment(failures(14, 50, 40))}, &Budget{}, logPath, time.Second)
	ls := outLines(out)
	// header, 10 × (name + 30 + marker), the more-failing line, the log line
	if want := 1 + 10*32 + 2; len(ls) != want {
		t.Fatalf("%d lines, want %d:\n%s", len(ls), want, out)
	}
	line := "\t" + strings.Repeat("x", 39)
	for i := 0; i < 10; i++ {
		blk := ls[1+i*32 : 1+(i+1)*32]
		if blk[0] != fmt.Sprintf("✗ fail-%03d", i) {
			t.Fatalf("block %d name line %q", i, blk[0])
		}
		for _, l := range blk[1:31] {
			if l != line {
				t.Fatalf("block %d output line %q", i, l)
			}
		}
		if want := "… 20 " + MoreLinesPrefix + " " + logPath; blk[31] != want {
			t.Fatalf("block %d marker %q, want %q", i, blk[31], want)
		}
	}
	if want := "+4 " + MoreFailingPrefix + " " + logPath; ls[len(ls)-2] != want {
		t.Fatalf("more-failing line %q, want %q", ls[len(ls)-2], want)
	}
}

func TestMassBreakSize(t *testing.T) {
	withParsers(t, newFake)
	s := NewSegment()
	line := []byte("\t" + strings.Repeat("y", 1999) + "\n")
	for i := 0; i < 500; i++ {
		fmt.Fprintf(s, "FAIL fail-%03d\n", i)
		for j := 0; j < 100; j++ {
			s.Write(line)
		}
	}
	s.Close()
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{s}, &Budget{}, logPath, time.Second)
	if n := strings.Count(out, "\n✗ "); n != 10 {
		t.Fatalf("%d failure blocks, want exactly 10", n)
	}
	if n := len(outLines(out)); n > 330 {
		t.Fatalf("%d lines, want ≤ 330", n)
	}
	if len(out) > 30000 {
		t.Fatalf("%d bytes, want ≤ 30,000", len(out))
	}
	if !hasLine(out, "+490 "+MoreFailingPrefix+" "+logPath) {
		t.Fatalf("no +490 more-failing line:\n%s", out[len(out)-400:])
	}
}

func TestBlockByteShare(t *testing.T) {
	withParsers(t, newFake)
	t.Run("long lines stop at the byte share", func(t *testing.T) {
		out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{segment(failures(1, 30, 240))}, &Budget{}, logPath, time.Second)
		ls := outLines(out)
		fit := BlockBytes / 241
		if ls[1] != "✗ fail-000" {
			t.Fatalf("name line %q", ls[1])
		}
		for _, l := range ls[2 : 2+fit] {
			if len(l) != 240 {
				t.Fatalf("output line of %d bytes, want whole 240-byte lines", len(l))
			}
		}
		if want := fmt.Sprintf("… %d %s %s", 30-fit, MoreLinesPrefix, logPath); ls[2+fit] != want {
			t.Fatalf("after %d lines got %q, want %q", fit, ls[2+fit], want)
		}
	})
	t.Run("short lines all fit", func(t *testing.T) {
		out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{segment(failures(1, 30, 60))}, &Budget{}, logPath, time.Second)
		ls := outLines(out)
		if len(ls) != 1+1+30+1 || strings.Contains(out, MoreLinesPrefix) {
			t.Fatalf("want header, name, all 30 lines and the log line:\n%s", out)
		}
	})
}

func TestLongLineKeepsParsing(t *testing.T) {
	var fed []string
	withParsers(t, func() Parser { return &fake{fed: &fed} })
	s := NewSegment()
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	for i := 0; i < 10; i++ {
		if n, err := s.Write(chunk); n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	s.Write([]byte("\nSENTINEL\n"))
	s.Close()
	if len(fed) != 2 || fed[1] != "SENTINEL" {
		t.Fatalf("fed %d lines, want the cut long line then SENTINEL", len(fed))
	}
	if len(fed[0]) != MaxLine {
		t.Fatalf("the long line reached the parser as %d bytes, want it cut at %d", len(fed[0]), MaxLine)
	}
}

func TestBoundedRetention(t *testing.T) {
	withParsers(t)
	s := NewSegment()
	var b []byte
	const total = 2_000_000
	for i := 0; i < total; i++ {
		b = fmt.Appendf(b, "line %d\n", i)
		if len(b) > 1<<20 {
			s.Write(b)
			b = b[:0]
		}
	}
	s.Write(b)
	s.Close()
	if r := s.Retained(); r > 1<<20 {
		t.Fatalf("Retained() = %d, want ≤ 1 MiB", r)
	}
	tail := s.Tail()
	if len(tail) != TailLines {
		t.Fatalf("tail holds %d lines, want %d", len(tail), TailLines)
	}
	for i, l := range tail {
		if want := fmt.Sprintf("line %d", total-TailLines+i); l != want {
			t.Fatalf("tail[%d] = %q, want %q", i, l, want)
		}
	}
}

func TestStripsANSIAndCR(t *testing.T) {
	var fed []string
	withParsers(t, func() Parser { return &fake{fed: &fed} })
	segment("\x1b[31;1mFAIL\x1b[0m\r\n")
	if len(fed) != 1 || fed[0] != "FAIL" {
		t.Fatalf("fed %q, want exactly \"FAIL\"", fed)
	}
	// A progress redraw: only what follows the last CR is what the terminal showed.
	fed = nil
	segment("50%\r100% done\r\n")
	if len(fed) != 1 || fed[0] != "100% done" {
		t.Fatalf("fed %q, want exactly %q", fed, "100% done")
	}
}

func TestAmbiguousClaimFallsBack(t *testing.T) {
	withParsers(t, newFake, newFake)
	out := Render(Outcome{Exit: 0, Ran: true}, []*Segment{segment("ok a\nok b\n")}, &Budget{}, logPath, time.Second)
	if !strings.HasPrefix(out, "exit 0 ") || strings.Contains(out, "PASS") || strings.Contains(out, "2 ok") {
		t.Fatalf("two claims did not fall back:\n%s", out)
	}
}

func TestBudgetSpansSegments(t *testing.T) {
	withParsers(t, newFake)
	text := failures(8, 2, 20)
	t.Run("one render", func(t *testing.T) {
		out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{segment(text), segment(text)}, &Budget{}, logPath, time.Second)
		if n := strings.Count(out, "\n✗ "); n != 10 {
			t.Fatalf("%d full blocks, want 10", n)
		}
		if !hasLine(out, "+6 "+MoreFailingPrefix+" "+logPath) {
			t.Fatalf("no +6 more-failing line:\n%s", out)
		}
	})
	t.Run("a render per lane", func(t *testing.T) {
		b := &Budget{}
		first := Render(Outcome{Exit: 1, Ran: true}, []*Segment{segment(text)}, b, logPath, time.Second)
		second := Render(Outcome{Exit: 1, Ran: true}, []*Segment{segment(text)}, b, logPath, time.Second)
		if n := strings.Count(first, "\n✗ ") + strings.Count(second, "\n✗ "); n != 10 {
			t.Fatalf("%d full blocks across both renders, want 10", n)
		}
		if strings.Contains(first, MoreFailingPrefix) || !hasLine(second, "+6 "+MoreFailingPrefix+" "+logPath) {
			t.Fatalf("want the +6 line on the second render only:\n%s\n---\n%s", first, second)
		}
	})
}

func TestGreenIsShort(t *testing.T) {
	withParsers(t, newFake)
	var b strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "ok pkg/n%02d 0.1s\n", i)
	}
	out := Render(Outcome{Exit: 0, Ran: true}, []*Segment{segment(b.String())}, &Budget{}, logPath, 72*time.Second)
	if n := len(outLines(out)); n > 3 {
		t.Fatalf("green is %d lines, want ≤ 3:\n%s", n, out)
	}
	if !strings.Contains(out, "60 ok") || !strings.Contains(out, "1m12s") || !strings.Contains(out, logPath) {
		t.Fatalf("green lacks the counts, the wall-clock or the log path:\n%s", out)
	}
	if strings.Contains(out, "pkg/") {
		t.Fatalf("green names a package:\n%s", out)
	}
}

func TestClockFormat(t *testing.T) {
	for d, want := range map[time.Duration]string{
		72 * time.Second:        "1m12s",
		3240 * time.Millisecond: "3.2s",
		75*time.Second + 400e6:  "1m15s",
	} {
		if got := clock(d); got != want {
			t.Errorf("clock(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestCutKeepsRunes(t *testing.T) {
	s := strings.Repeat("a", LineCut-1) + "é"
	if got := Cut(s); got != strings.Repeat("a", LineCut-1) {
		t.Fatalf("Cut split a rune: %q", got[len(got)-3:])
	}
}
