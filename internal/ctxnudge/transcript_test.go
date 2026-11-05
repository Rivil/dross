package ctxnudge

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// assistantLine renders a transcript assistant entry with only the fields this
// package reads, plus output_tokens, which must never count.
func assistantLine(sidechain bool, model string, input, cacheRead, cacheCreation int64) string {
	return fmt.Sprintf(`{"type":"assistant","isSidechain":%t,"message":{"model":%q,"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d,"output_tokens":9999}}}`,
		sidechain, model, input, cacheRead, cacheCreation)
}

// mainLine is a main-agent assistant entry whose context is exactly tokens.
func mainLine(tokens int64) string { return assistantLine(false, "claude-opus-5-5", 2, tokens-2, 0) }

// userLine is a tool-result entry padded to roughly n bytes; it carries no usage.
func userLine(n int) string {
	return `{"type":"user","isSidechain":false,"message":{"role":"user","content":"` + strings.Repeat("x", n) + `"}}`
}

// writeTranscript writes lines newline-terminated, as Claude Code appends them.
func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func wantContext(t *testing.T, path string, want int64) {
	t.Helper()
	got, ok := LatestMainContext(path)
	if !ok || got != want {
		t.Fatalf("LatestMainContext = (%d, %v), want (%d, true)", got, ok, want)
	}
}

func wantNone(t *testing.T, path string) {
	t.Helper()
	if got, ok := LatestMainContext(path); ok {
		t.Fatalf("LatestMainContext = (%d, true), want ok=false", got)
	}
}

// TestRealMainTail reads a jq projection of a real main-session tail
// ({type,isSidechain,message:{model,usage}} only, no content): the newest
// main-agent turn's three input fields sum to 64,719.
func TestRealMainTail(t *testing.T) {
	wantContext(t, filepath.Join("testdata", "main_tail.jsonl"), 64_719)
}

// TestSubagentTranscriptYieldsNothing: a subagents/agent-*.jsonl file holds only
// sidechain entries, so a hook handed one must not read a number from it.
func TestSubagentTranscriptYieldsNothing(t *testing.T) {
	wantNone(t, filepath.Join("testdata", "subagent_tail.jsonl"))
}

// TestMainBeatsNewerSidechain: a newer sidechain turn never counts — only the
// main agent's context decides whether the session is over threshold (c-2).
func TestMainBeatsNewerSidechain(t *testing.T) {
	wantContext(t, writeTranscript(t,
		mainLine(120_000),
		assistantLine(true, "claude-opus-5-5", 2, 189_998, 0),
	), 120_000)
}

// TestUsageSumsThreeInputFields pins the sum: input + cache-read +
// cache-creation, never output, and a missing cache-creation key counts as 0.
func TestUsageSumsThreeInputFields(t *testing.T) {
	wantContext(t, writeTranscript(t, assistantLine(false, "claude-opus-5-5", 2, 151_000, 407)), 151_409)
	wantContext(t, writeTranscript(t,
		`{"type":"assistant","isSidechain":false,"message":{"model":"claude-opus-5-5","usage":{"input_tokens":5,"cache_read_input_tokens":100,"output_tokens":9999}}}`,
	), 105)
}

// TestSyntheticEntrySkipped: Claude Code writes "<synthetic>" placeholder turns
// with zero usage; the real turn before one is the session's context, not 0.
func TestSyntheticEntrySkipped(t *testing.T) {
	wantContext(t, writeTranscript(t,
		mainLine(90_000),
		assistantLine(false, "<synthetic>", 0, 0, 0),
	), 90_000)
	// The model check stands on its own: a synthetic entry carrying usage is
	// still not the session's context.
	wantContext(t, writeTranscript(t,
		mainLine(95_000),
		assistantLine(false, "<synthetic>", 2, 300_000, 0),
	), 95_000)
	// A zero-usage real entry is no evidence either.
	wantContext(t, writeTranscript(t,
		mainLine(80_000),
		assistantLine(false, "claude-opus-5-5", 0, 0, 0),
	), 80_000)
}

// TestTornTailSkipped: the hook can fire while Claude Code is mid-append, so the
// last line may have no closing brace and no newline.
func TestTornTailSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	torn := mainLine(170_000)
	body := mainLine(110_000) + "\n" + torn[:len(torn)-5]
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	wantContext(t, path, 110_000)
}

// TestLongLineAfterLatestEntry: a tool result can be one multi-megabyte line;
// a line-length cap (bufio.Scanner's 64 KiB default) would lose the entry
// behind it.
func TestLongLineAfterLatestEntry(t *testing.T) {
	wantContext(t, writeTranscript(t, mainLine(130_000), userLine(3<<20)), 130_000)
}

// TestSpacedJSONRecognised: the read is a JSON decode, not a byte match, so
// spacing after the colons is still an assistant entry.
func TestSpacedJSONRecognised(t *testing.T) {
	wantContext(t, writeTranscript(t,
		`{"type": "assistant", "isSidechain": false, "message": {"model": "claude-opus-5-5", "usage": {"input_tokens": 1, "cache_read_input_tokens": 140000, "cache_creation_input_tokens": 9}}}`,
	), 140_010)
}

// TestSaturatingSum: three huge fields pin at MaxInt64 rather than wrapping to
// a negative that would read as "under every threshold".
func TestSaturatingSum(t *testing.T) {
	const big = 4_000_000_000_000_000_000
	wantContext(t, writeTranscript(t, assistantLine(false, "claude-opus-5-5", big, big, big)), math.MaxInt64)
}

// TestCapReportsUnknownNotZero: no qualifying entry within readCap is "unknown"
// (ok=false), never (0, true) — a false zero would silence the nudge for good.
func TestCapReportsUnknownNotZero(t *testing.T) {
	lines := []string{mainLine(200_000)}
	for range (readCap / (64 << 10)) + 8 {
		lines = append(lines, userLine(64<<10))
	}
	wantNone(t, writeTranscript(t, lines...))
}

// TestSpecialFilesAreSilent: a non-regular file at the transcript path neither
// blocks nor errors loudly. A FIFO with no writer would hang a blocking open.
func TestSpecialFilesAreSilent(t *testing.T) {
	dir := t.TempDir()
	wantNone(t, dir)
	wantNone(t, filepath.Join(dir, "missing.jsonl"))

	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("mkfifo"); err == nil {
			fifo := filepath.Join(dir, "fifo.jsonl")
			if out, err := exec.Command("mkfifo", fifo).CombinedOutput(); err != nil {
				t.Fatalf("mkfifo: %v: %s", err, out)
			}
			done := make(chan bool, 1)
			go func() { _, ok := LatestMainContext(fifo); done <- ok }()
			select {
			case ok := <-done:
				if ok {
					t.Error("a FIFO yielded a context")
				}
			case <-time.After(time.Second):
				t.Fatal("LatestMainContext blocked on a FIFO with no writer")
			}
		}
	}

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return
	}
	locked := writeTranscript(t, mainLine(160_000))
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o600) })
	wantNone(t, locked)
}

// countingReaderAt records how many bytes a scan pulled from the file.
type countingReaderAt struct {
	r io.ReaderAt
	n atomic.Int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.n.Add(int64(n))
	return n, err
}

// bigTranscript writes a 50 MB sparse transcript whose newest main-agent entry
// sits 200 KiB from EOF, behind tool-result lines.
func bigTranscript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "big.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var tail strings.Builder
	tail.WriteString("\n" + mainLine(175_000) + "\n")
	for tail.Len() < 200<<10 {
		tail.WriteString(userLine(8<<10) + "\n")
	}
	const size = 50 << 20
	if _, err := f.WriteAt([]byte(tail.String()), size-int64(tail.Len())); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestTailReadIsBounded: the scan reads the tail, not the file (c-7).
func TestTailReadIsBounded(t *testing.T) {
	path := bigTranscript(t)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	c := &countingReaderAt{r: f}
	got, ok := latestMain(c, info.Size())
	if !ok || got != 175_000 {
		t.Fatalf("latestMain = (%d, %v), want (175000, true)", got, ok)
	}
	if n := c.n.Load(); n > 1<<20 {
		t.Errorf("read %d bytes of a 50 MB transcript; want ≤ 1 MiB", n)
	}
}

// TestTailRead50MBUnder50ms: the hook's budget is 50 ms per fire on a 50 MB
// transcript (c-7). The minimum of five runs discounts scheduler noise.
func TestTailRead50MBUnder50ms(t *testing.T) {
	path := bigTranscript(t)
	best := time.Duration(math.MaxInt64)
	for range 5 {
		start := time.Now()
		if _, ok := LatestMainContext(path); !ok {
			t.Fatal("no context found")
		}
		best = min(best, time.Since(start))
	}
	if best >= 50*time.Millisecond {
		t.Errorf("best of 5 reads took %v; budget is 50ms", best)
	}
}
