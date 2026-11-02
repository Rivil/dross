package runsummary

import (
	"bytes"
	"fmt"
	"reflect"
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
	longLog := "/" + strings.Repeat("l", 199)
	for i := 0; i < 500; i++ {
		fmt.Fprintf(s, "FAIL fail-%03d-%s\n", i, strings.Repeat("n", 300))
		for j := 0; j < 100; j++ {
			s.Write(line)
		}
	}
	s.Close()
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{s}, &Budget{}, longLog, time.Second)
	if n := strings.Count(out, "\n✗ "); n != 10 {
		t.Fatalf("%d failure blocks, want exactly 10", n)
	}
	for i, b := range printedBlocks(out) {
		if len(b) > BlockBytes {
			t.Errorf("block %d prints %d bytes, want ≤ %d", i, len(b), BlockBytes)
		}
	}
	if n := len(outLines(out)); n > 330 {
		t.Fatalf("%d lines, want ≤ 330", n)
	}
	if len(out) > 30000 {
		t.Fatalf("%d bytes, want ≤ 30,000", len(out))
	}
	if !hasLine(out, "+490 "+MoreFailingPrefix+" "+longLog) {
		t.Fatalf("no +490 more-failing line:\n%s", out[len(out)-400:])
	}
}

func TestBlockByteShare(t *testing.T) {
	withParsers(t, newFake)
	t.Run("long lines stop at the byte share", func(t *testing.T) {
		out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{segment(failures(1, 30, 240))}, &Budget{}, logPath, time.Second)
		ls := outLines(out)
		// The name and the marker count toward the block's bytes too.
		name := len("✗ fail-000") + 1
		marker := len(fmt.Sprintf("… %d %s %s", 30, MoreLinesPrefix, logPath)) + 1
		fit := (BlockBytes - name - marker) / 241
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

// printedBlocks splits rendered output into its failure blocks, each from its
// "✗ " line through the line before the next block or the closing lines.
func printedBlocks(out string) []string {
	var blocks []string
	var cur *strings.Builder
	for _, l := range outLines(out) {
		switch {
		case strings.HasPrefix(l, "✗ "):
			if cur != nil {
				blocks = append(blocks, cur.String())
			}
			cur = &strings.Builder{}
		case cur == nil:
			continue
		case strings.HasPrefix(l, "log: ") || strings.Contains(l, " "+MoreFailingPrefix+" "):
			blocks = append(blocks, cur.String())
			cur = nil
			continue
		}
		cur.WriteString(l + "\n")
	}
	if cur != nil {
		blocks = append(blocks, cur.String())
	}
	return blocks
}

// renderOne renders a single failure through Render.
func renderOne(f Failure, log string) string {
	p := &fake{claimed: true}
	p.fs.Add(f)
	s := &Segment{parsers: []Parser{p}}
	s.Close()
	return Render(Outcome{Exit: 1, Ran: true}, []*Segment{s}, &Budget{}, log, time.Second)
}

// fullLines is a failure holding n kept lines of width bytes each.
func fullLines(name string, n, width int) Failure {
	f := Failure{Name: name}
	for i := 0; i < n; i++ {
		f.Lines = append(f.Lines, strings.Repeat("z", width))
	}
	return f
}

// TestWorstCaseBlockBytes: names past LineCut, every line at LineCut and a
// 200-byte log path still print every block, ✗ through marker, within
// BlockBytes — and ten blocks stay under 30 KB.
func TestWorstCaseBlockBytes(t *testing.T) {
	withParsers(t, newFake)
	longLog := "/" + strings.Repeat("l", 199)
	var b strings.Builder
	for i := 0; i < 14; i++ {
		fmt.Fprintf(&b, "FAIL fail-%03d-%s\n", i, strings.Repeat("n", 400))
		for j := 0; j < 40; j++ {
			b.WriteString("\t" + strings.Repeat("x", LineCut-1) + "\n")
		}
	}
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{segment(b.String())}, &Budget{}, longLog, time.Second)
	blocks := printedBlocks(out)
	if len(blocks) != 10 || strings.Count(out, "\n✗ ") != 10 {
		t.Fatalf("%d blocks, want exactly 10", len(blocks))
	}
	for i, blk := range blocks {
		if len(blk) > BlockBytes {
			t.Errorf("block %d prints %d bytes, want ≤ %d", i, len(blk), BlockBytes)
		}
	}
	if len(out) > 30000 {
		t.Fatalf("%d bytes, want ≤ 30,000", len(out))
	}
}

// TestBlockNameCounts: a long name costs output lines.
func TestBlockNameCounts(t *testing.T) {
	short := renderOne(fullLines("n", 30, 200), logPath)
	long := renderOne(fullLines(strings.Repeat("n", 240), 30, 200), logPath)
	if a, b := len(outLines(long)), len(outLines(short)); a >= b {
		t.Fatalf("a 240-byte name prints %d lines, a 1-byte one %d; want strictly fewer", a, b)
	}
}

// TestBlockMarkerCounts: a long log path in the marker costs output lines.
func TestBlockMarkerCounts(t *testing.T) {
	short := renderOne(fullLines("n", 30, 200), "/l")
	long := renderOne(fullLines("n", 30, 200), "/"+strings.Repeat("l", 199))
	if a, b := len(outLines(long)), len(outLines(short)); a >= b {
		t.Fatalf("a 200-byte log path prints %d lines, a 2-byte one %d; want strictly fewer", a, b)
	}
}

// TestBlockBoundCountsDropped: lines the bound drops join the marker's count.
func TestBlockBoundCountsDropped(t *testing.T) {
	f := fullLines("n", 25, 200)
	f.More = 7
	out := renderOne(f, logPath)
	kept := 0
	for _, l := range outLines(out) {
		if l == strings.Repeat("z", 200) {
			kept++
		}
	}
	want := fmt.Sprintf("… %d %s %s", 7+25-kept, MoreLinesPrefix, logPath)
	if kept == 25 || !hasLine(out, want) {
		t.Fatalf("kept %d of 25, want some dropped and the marker %q:\n%s", kept, want, out)
	}
}

// TestBlockBoundKeepsSmallBlock: a block that fits prints unchanged.
func TestBlockBoundKeepsSmallBlock(t *testing.T) {
	out := renderOne(Failure{Name: "small", Lines: []string{"a", "b", "c"}}, logPath)
	ls := outLines(out)
	if !reflect.DeepEqual(ls[1:5], []string{"✗ small", "a", "b", "c"}) || strings.Contains(out, MoreLinesPrefix) {
		t.Fatalf("small block changed:\n%s", out)
	}
}

// TestSharedHeaderCopyBounded: vitest prints one shared error under several
// FAIL headers; each printed copy, re-stamped with its own long name, stays
// within BlockBytes.
func TestSharedHeaderCopyBounded(t *testing.T) {
	v := &vitest{}
	v.Feed(" Tests  3 failed (3)")
	for i := 0; i < 3; i++ {
		v.Feed(fmt.Sprintf(" FAIL  src/x.test.ts > case %d %s", i, strings.Repeat("n", 240)))
	}
	for i := 0; i < 40; i++ {
		v.Feed(strings.Repeat("e", LineCut))
	}
	v.Feed("⎯⎯⎯⎯[1/3]⎯")
	s := &Segment{parsers: []Parser{v}}
	s.Close()
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{s}, &Budget{}, "/"+strings.Repeat("l", 199), time.Second)
	blocks := printedBlocks(out)
	if len(blocks) != 3 {
		t.Fatalf("%d blocks, want the 3 shared copies:\n%s", len(blocks), out)
	}
	for i, b := range blocks {
		if len(b) > BlockBytes {
			t.Errorf("copy %d prints %d bytes, want ≤ %d", i, len(b), BlockBytes)
		}
	}
}
