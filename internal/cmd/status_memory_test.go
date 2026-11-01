package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/memadvise"
)

// memoryFixtureTurns is the repo's assistant-turn count in the fixture
// transcript. It is large enough that the five index lines below price at
// distinct cents, so the $/month order is visible in the rendered figures.
const memoryFixtureTurns = 2000

// memoryFixtureTokens is each dead index link's stem and the tokens its line
// costs. The index lists them a..e and the IDs sort a..e, so neither order is
// the $/month order: b, d, a, e, c.
var memoryFixtureTokens = []struct {
	stem   string
	tokens int
}{{"a", 300}, {"b", 500}, {"c", 100}, {"d", 400}, {"e", 200}}

// memoryDir is the Claude memory dir for the fixture repo at dir, under the
// per-test CLAUDE_CONFIG_DIR chdir pins. It is not created.
func memoryDir(t *testing.T, dir string) string {
	t.Helper()
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	if cfg == "" {
		t.Fatal("chdir left CLAUDE_CONFIG_DIR unset; the fixture would land in the hermetic HOME")
	}
	return filepath.Join(cfg, "projects", memadvise.Slug(dir), "memory")
}

// memoryIndexLine is a dead index link to stem.md whose line, newline
// included, is exactly tokens*4 bytes.
func memoryIndexLine(stem string, tokens int) string {
	head := fmt.Sprintf("- [%s](%s.md) — ", stem, stem)
	return head + strings.Repeat("x", tokens*4-1-len(head))
}

// fiveItemMemory writes the five-dead-link MEMORY.md and a transcript holding
// memoryFixtureTurns of the repo's assistant turns, and returns the memory dir.
func fiveItemMemory(t *testing.T, dir string) string {
	t.Helper()
	mem := memoryDir(t, dir)
	var index strings.Builder
	for _, f := range memoryFixtureTokens {
		index.WriteString(memoryIndexLine(f.stem, f.tokens) + "\n")
	}
	mustWrite(t, filepath.Join(mem, memadvise.IndexName), index.String())
	var tr strings.Builder
	ts := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	for i := 0; i < memoryFixtureTurns; i++ {
		fmt.Fprintf(&tr, `{"type":"assistant","timestamp":%q,"message":{"id":"msg_%d","model":"claude-opus-5-5"}}`+"\n", ts, i)
	}
	mustWrite(t, filepath.Join(filepath.Dir(mem), "session.jsonl"), tr.String())
	return mem
}

// memorySection returns status's memory: block — the label line and every
// indented line under it — or nil when there is none.
func memorySection(status string) []string {
	lines := strings.Split(status, "\n")
	for i, l := range lines {
		if l != "memory:" {
			continue
		}
		block := []string{l}
		for _, b := range lines[i+1:] {
			if !strings.HasPrefix(b, "  ") {
				break
			}
			block = append(block, b)
		}
		return block
	}
	return nil
}

// lineIndex is the index of the first line of s with prefix, or -1.
func lineIndex(s, prefix string) int {
	for i, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, prefix) {
			return i
		}
	}
	return -1
}

// advisorOff turns the memory section off for the rest of the test.
func advisorOff(t *testing.T) {
	t.Helper()
	prev := statusMemory
	statusMemory = func(string, time.Time) string { return "" }
	t.Cleanup(func() { statusMemory = prev })
}

// runStatusErr runs status and returns its output and error, never failing.
func runStatusErr(t *testing.T) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Status()) })
	return out, err
}

func TestStatusMemoryTopThree(t *testing.T) {
	dir := debugNudgeFixture(t)
	fiveItemMemory(t, dir)

	block := memorySection(runStatus(t))
	if block == nil {
		t.Fatal("status printed no memory: section for a five-item memory dir")
	}
	if len(block) < 5 {
		t.Fatalf("memory section too short:\n%s", strings.Join(block, "\n"))
	}
	byStem := map[string]int{}
	for _, f := range memoryFixtureTokens {
		byStem[f.stem] = f.tokens
	}
	for i, stem := range []string{"b", "d", "a"} {
		line := block[1+i]
		if !strings.HasPrefix(line, "  delete-"+stem+"  ") {
			t.Errorf("item %d = %q, want delete-%s (the %s highest $/month)", i+1, line, stem, []string{"1st", "2nd", "3rd"}[i])
		}
		usd := fmt.Sprintf("$%.2f/mo", memadvise.MonthlyUSD(byStem[stem]*4, memoryFixtureTurns))
		if !strings.Contains(line, usd) {
			t.Errorf("item %d = %q, want %s", i+1, line, usd)
		}
	}
	if got, want := block[4], "  +2 more — dross memory advise"; got != want {
		t.Errorf("line after the top three = %q, want %q", got, want)
	}
	for _, stem := range []string{"c", "e"} {
		for _, l := range block {
			if strings.HasPrefix(l, "  delete-"+stem+"  ") {
				t.Errorf("status listed a fourth item %q; only the top three belong there", l)
			}
		}
	}
}

func TestStatusMemorySilentWhenNothing(t *testing.T) {
	dir := debugNudgeFixture(t)
	mem := memoryDir(t, dir)

	advisorOff(t)
	off := runStatus(t)
	if strings.Contains(off, "memory:") {
		t.Fatalf("the advisor-off baseline has a memory section:\n%s", off)
	}
	statusMemory = memoryAdvice

	if got := runStatus(t); got != off {
		t.Errorf("no memory dir changed status:\n--- off\n%s--- on\n%s", off, got)
	}

	if err := os.MkdirAll(mem, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := runStatus(t); got != off {
		t.Errorf("an empty memory dir changed status:\n--- off\n%s--- on\n%s", off, got)
	}

	// An index whose every line points at a memory that duplicates nothing
	// and carries no expired clause: a real dir with nothing to suggest.
	mustWrite(t, filepath.Join(mem, memadvise.IndexName), "- [Kept](kept.md) — a fact the repo's docs do not hold\n")
	mustWrite(t, filepath.Join(mem, "kept.md"), "---\nname: kept\n---\nthe deploy host is called helicon\n")
	if got := runStatus(t); got != off {
		t.Errorf("a memory dir with nothing to suggest changed status:\n--- off\n%s--- on\n%s", off, got)
	}
}

// memDirHash is sha256 over every entry's path, mode and bytes under dir.
func memDirHash(t *testing.T, dir string) string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, p)
		h.Write([]byte(rel + "\x00" + info.Mode().String() + "\x00"))
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestStatusNeverWritesMemoryDir(t *testing.T) {
	dir := debugNudgeFixture(t)
	mem := fiveItemMemory(t, dir)
	// An orphan and an indexed memory too, so every kind of file is present.
	mustWrite(t, filepath.Join(mem, "lost.md"), "---\nname: lost\n---\nnot in the index\n")
	before := memDirHash(t, mem)

	// First run counts turns from scratch, second reuses the cache; then a
	// recount again with the cache gone. None may touch the memory dir.
	for i := 0; i < 2; i++ {
		if memorySection(runStatus(t)) == nil {
			t.Fatalf("run %d printed no memory section; the advisor did not run", i+1)
		}
	}
	cache := filepath.Join(dir, filepath.FromSlash(memadvise.TurnCachePath))
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("the turn cache was not written under the repo: %v", err)
	}
	if err := os.Remove(cache); err != nil {
		t.Fatal(err)
	}
	runStatus(t)

	if after := memDirHash(t, mem); after != before {
		t.Error("dross status changed the memory dir")
	}
}

func TestStatusMemoryErrorIsSilent(t *testing.T) {
	check := func(t *testing.T) {
		t.Helper()
		out, err := runStatusErr(t)
		if err != nil {
			t.Fatalf("status exited with %v", err)
		}
		if l := lineIndex(out, "memory:"); l >= 0 {
			t.Errorf("status printed a memory section on an advisor failure:\n%s", out)
		}
		if !strings.HasPrefix(lastLine(out), "you are here: ") {
			t.Errorf("status lost its footer: %q", lastLine(out))
		}
	}

	if os.Geteuid() == 0 {
		t.Skip("root reads chmod-000 files and dirs")
	}

	t.Run("unreadable MEMORY.md", func(t *testing.T) {
		dir := debugNudgeFixture(t)
		mem := fiveItemMemory(t, dir)
		mustWrite(t, filepath.Join(mem, "lost.md"), "---\nname: lost\n---\nan orphan once the index is gone\n")
		idx := filepath.Join(mem, memadvise.IndexName)
		if err := os.Chmod(idx, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(idx, 0o644) })
		check(t)
	})

	t.Run("unreadable memory dir", func(t *testing.T) {
		dir := debugNudgeFixture(t)
		mem := fiveItemMemory(t, dir)
		if err := os.Chmod(mem, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(mem, 0o755) })
		check(t)
	})
}

// TestStatusMemoryOnCacheWriteError: a turn cache that cannot be written — a
// read-only checkout — costs only the cache. The counts are sound, so status
// still prints its memory section, stays quiet about the failed write (advise
// reports it) and exits 0 with its footer last.
func TestStatusMemoryOnCacheWriteError(t *testing.T) {
	dir := debugNudgeFixture(t)
	fiveItemMemory(t, dir)
	// A regular file where the advisor's state dir goes.
	mustWrite(t, filepath.Join(dir, filepath.FromSlash(memadvise.StateDir)), "not a directory\n")
	out, err := runStatusErr(t)
	if err != nil {
		t.Fatalf("status exited with %v", err)
	}
	block := memorySection(out)
	if len(block) < 2 || !strings.HasPrefix(block[1], "  delete-b  ") {
		t.Errorf("status on an unwritable turn cache lost its memory section:\n%s", out)
	}
	if strings.Contains(out, "could not be cached") {
		t.Errorf("status reported the cache write; only advise does:\n%s", out)
	}
	if !strings.HasPrefix(lastLine(out), "you are here: ") {
		t.Errorf("status lost its footer: %q", lastLine(out))
	}
}

func TestStatusFooterStillLast(t *testing.T) {
	dir := debugNudgeFixture(t)
	fiveItemMemory(t, dir)
	debugAt(t, dir, "flaky-login", "open", nil, debugBase)

	out, reentry := runStatus(t), runReentry(t)
	debugLn, memAt, footerAt := lineIndex(out, "debug:"), lineIndex(out, "memory:"), lineIndex(out, "you are here: ")
	if memAt < 0 {
		t.Fatalf("no memory section:\n%s", out)
	}
	if debugLn < 0 || debugLn > memAt {
		t.Errorf("memory section (line %d) is not after the debug line (line %d):\n%s", memAt, debugLn, out)
	}
	if footerAt < memAt {
		t.Errorf("the footer (line %d) comes before the memory section (line %d):\n%s", footerAt, memAt, out)
	}
	if lastLine(out) != reentry {
		t.Fatalf("status's last line %q != the hook's %q", lastLine(out), reentry)
	}
	if strings.Contains(reentry, "memory") {
		t.Errorf("the memory advice leaked into the re-entry line: %q", reentry)
	}
}
