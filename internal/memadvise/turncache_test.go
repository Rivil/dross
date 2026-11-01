package memadvise

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cacheFixture is a repo with one transcript of two turns, counted once at
// now so turns.json exists.
func cacheFixture(t *testing.T) (root, transcript string, now time.Time) {
	t.Helper()
	claude, root := turnsEnv(t)
	now = time.Now()
	at := now.Add(-time.Hour)
	transcript = filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl")
	writeTranscript(t, transcript, turn(t, "msg_a", at), turn(t, "msg_b", at))
	if c := countTurns(t, root, now); c.Repo != 2 || c.Age != 0 {
		t.Fatalf("first count Repo=%d Age=%v, want 2 and 0", c.Repo, c.Age)
	}
	return root, transcript, now
}

func readCache(t *testing.T, root string, now time.Time) turnCache {
	t.Helper()
	c, ok := loadCache(filepath.Join(root, filepath.FromSlash(TurnCachePath)), now)
	if !ok {
		t.Fatal("turns.json is missing or untrusted")
	}
	return c
}

func TestCacheReusedUnder24h(t *testing.T) {
	root, transcript, now := cacheFixture(t)
	appendTranscript(t, transcript, turn(t, "msg_c", now))
	c := countTurns(t, root, now.Add(23*time.Hour))
	if c.Repo != 2 {
		t.Errorf("a 23h-old cache showed Repo=%d, want the cached 2 — the appended turn waits for the recount", c.Repo)
	}
	if c.Age != 23*time.Hour {
		t.Errorf("Age = %v, want 23h", c.Age)
	}
	if c.Scanned != 0 {
		t.Errorf("a cache hit read %d transcript bytes, want 0", c.Scanned)
	}
}

func TestCacheRecountedAt24h(t *testing.T) {
	root, transcript, now := cacheFixture(t)
	appendTranscript(t, transcript, turn(t, "msg_c", now))
	later := now.Add(25 * time.Hour)
	c := countTurns(t, root, later)
	if c.Repo != 3 || c.Age != 0 {
		t.Errorf("a 25h-old cache gave Repo=%d Age=%v, want the recounted 3 and 0", c.Repo, c.Age)
	}
	if got := readCache(t, root, later).CountedAt; !got.Equal(later) {
		t.Errorf("turns.json counted_at = %v, want it rewritten to %v", got, later)
	}
}

func TestCacheRecountsOnCorruptOrFuture(t *testing.T) {
	// forge writes a cache that claims 999 turns for the transcript as it
	// stands, so trusting it shows as Repo=999.
	forge := func(t *testing.T, root, transcript string, now, countedAt time.Time, version, days int) {
		t.Helper()
		info, err := os.Stat(transcript)
		if err != nil {
			t.Fatal(err)
		}
		key := Slug(root) + "/" + filepath.Base(transcript)
		err = saveCache(root, turnCache{Version: version, CountedAt: countedAt, Files: map[string]fileEntry{
			key: {Size: info.Size(), ModTime: info.ModTime().UnixNano(), Offset: info.Size(),
				Days: map[string]int{now.UTC().Format(time.DateOnly): days}},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}

	t.Run("control: a sound cache is trusted", func(t *testing.T) {
		root, transcript, now := cacheFixture(t)
		forge(t, root, transcript, now, now.Add(-time.Hour), turnCacheVersion, 999)
		if c := countTurns(t, root, now); c.Repo != 999 {
			t.Fatalf("Repo=%d, want the forged 999 — the cases below would prove nothing", c.Repo)
		}
	})
	for _, tc := range []struct {
		name      string
		countedAt time.Duration
		version   int
		days      int
	}{
		{"future-dated", time.Hour, turnCacheVersion, 999},
		{"another version", -time.Hour, turnCacheVersion + 1, 999},
		{"negative count", -time.Hour, turnCacheVersion, -999},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, transcript, now := cacheFixture(t)
			forge(t, root, transcript, now, now.Add(tc.countedAt), tc.version, tc.days)
			if c := countTurns(t, root, now); c.Repo != 2 || c.Age != 0 {
				t.Errorf("Repo=%d Age=%v, want a recount from the transcript: 2 and 0", c.Repo, c.Age)
			}
		})
	}
	t.Run("corrupt", func(t *testing.T) {
		root, _, now := cacheFixture(t)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(TurnCachePath)), []byte(`{"version":1,"files":{`), 0o644); err != nil {
			t.Fatal(err)
		}
		if c := countTurns(t, root, now); c.Repo != 2 || c.Age != 0 {
			t.Errorf("Repo=%d Age=%v, want a recount: 2 and 0", c.Repo, c.Age)
		}
	})
}

func TestIncrementalRecountAndTruncation(t *testing.T) {
	root, transcript, now := cacheFixture(t)
	appended := turn(t, "msg_c", now)
	appendTranscript(t, transcript, appended)
	c := countTurns(t, root, now.Add(25*time.Hour))
	if c.Repo != 3 {
		t.Fatalf("Repo=%d after one appended turn, want 3", c.Repo)
	}
	if c.Scanned != int64(len(appended)) {
		t.Errorf("the recount read %d bytes, want only the %d appended", c.Scanned, len(appended))
	}

	// Rewritten shorter than the cached offset: counted afresh from byte 0.
	writeTranscript(t, transcript, turn(t, "msg_x", now))
	info, err := os.Stat(transcript)
	if err != nil {
		t.Fatal(err)
	}
	c = countTurns(t, root, now.Add(50*time.Hour))
	if c.Repo != 1 || c.Scanned != info.Size() {
		t.Errorf("after truncation Repo=%d Scanned=%d, want 1 and the whole %d-byte file", c.Repo, c.Scanned, info.Size())
	}
}

func TestPartialTailLineCountsOnce(t *testing.T) {
	claude, root := turnsEnv(t)
	now := time.Now()
	at := now.Add(-time.Hour)
	transcript := filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl")
	// Mid-write: the last line has no newline yet.
	writeTranscript(t, transcript, turn(t, "msg_a", at), strings.TrimSuffix(turn(t, "msg_b", at), "\n"))
	if c := countTurns(t, root, now); c.Repo != 2 {
		t.Fatalf("Repo=%d, want 2 — an unterminated but whole line still counts", c.Repo)
	}
	// The write finishes and another turn follows: the re-read of msg_b's
	// line must not count it again.
	appendTranscript(t, transcript, "\n"+turn(t, "msg_c", at))
	if c := countTurns(t, root, now.Add(25*time.Hour)); c.Repo != 3 {
		t.Errorf("Repo=%d after the tail line completed, want 3", c.Repo)
	}
}

// memFingerprint is every path under dir with its bytes, so any write shows.
func memFingerprint(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		b.WriteString(p + "\n")
		if d.Type().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			b.Write(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestCacheNeverInMemoryDir(t *testing.T) {
	if !strings.HasPrefix(TurnCachePath, ".dross/memory/") {
		t.Fatalf("TurnCachePath = %q, want it under .dross/memory/", TurnCachePath)
	}
	claude, root := turnsEnv(t)
	now := time.Now()
	mem := memDir(t, claude, root)
	if err := os.WriteFile(filepath.Join(mem, IndexName), []byte("- [A](a.md) — hook\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl"), turn(t, "msg_a", now))
	before := memFingerprint(t, mem)

	countTurns(t, root, now)
	if _, err := os.Stat(filepath.Join(root, ".dross", "memory", "turns.json")); err != nil {
		t.Errorf("the cache is not at <root>/.dross/memory/turns.json: %v", err)
	}
	if after := memFingerprint(t, mem); after != before {
		t.Errorf("counting turns changed the memory dir:\nbefore %q\nafter  %q", before, after)
	}

	// A state dir linked into the memory dir must be refused, not written
	// through.
	root2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root2, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mem, filepath.Join(root2, filepath.FromSlash(StateDir))); err != nil {
		t.Fatal(err)
	}
	if _, err := Turns(root2, now); err == nil {
		t.Error("Turns wrote through a .dross/memory symlink without an error")
	}
	if after := memFingerprint(t, mem); after != before {
		t.Errorf("a linked state dir let the cache into the memory dir:\nbefore %q\nafter  %q", before, after)
	}

	// A parent link: .dross itself points at the memory dir's parent, so a
	// real-looking .dross/memory is the memory dir. Lstat of the last
	// component alone passes that; every component must be checked.
	root3 := t.TempDir()
	if err := os.Symlink(filepath.Dir(mem), filepath.Join(root3, ".dross")); err != nil {
		t.Fatal(err)
	}
	_, err := Turns(root3, now)
	if err == nil || !strings.Contains(err.Error(), ".dross is not a directory") {
		t.Errorf("Turns through a symlinked .dross = %v, want a refusal naming .dross", err)
	}
	if after := memFingerprint(t, mem); after != before {
		t.Errorf("a linked .dross let the cache into the memory dir:\nbefore %q\nafter  %q", before, after)
	}
}

// TestCacheWriteErrorKeepsCounts: a state dir that cannot be written costs
// only the cache. The counts come back whole, beside an ErrCacheWrite.
func TestCacheWriteErrorKeepsCounts(t *testing.T) {
	claude, root := turnsEnv(t)
	now := time.Now()
	writeTranscript(t, filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl"),
		turn(t, "msg_a", now.Add(-time.Hour)), turn(t, "msg_b", now.Add(-time.Hour)))
	writeRepoFile(t, root, StateDir, "not a dir\n")
	c, err := Turns(root, now)
	if !errors.Is(err, ErrCacheWrite) {
		t.Fatalf("Turns with a file at %s = %v, want an ErrCacheWrite", StateDir, err)
	}
	if c.Repo != 2 || c.All != 2 {
		t.Errorf("a failed cache write returned Repo=%d All=%d, want the counted 2 and 2", c.Repo, c.All)
	}

	// Any other failure is not a cache write and must not pass for one.
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "")
	if _, err := ClaudeDir(); err == nil {
		t.Skip("ClaudeDir resolves with HOME and CLAUDE_CONFIG_DIR unset")
	}
	if _, err := Turns(root, now); err == nil || errors.Is(err, ErrCacheWrite) {
		t.Errorf("Turns with no Claude dir = %v, want an error that is not ErrCacheWrite", err)
	}
}

// TestCacheKeepsUnreadable: a count that skipped an unreadable transcript is
// saved with that number, so a cache hit is exactly as partial as the count.
func TestCacheKeepsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-000 dir, so a failed read cannot be detected")
	}
	claude, root := turnsEnv(t)
	now := time.Now()
	writeTranscript(t, filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl"), turn(t, "msg_a", now.Add(-time.Hour)))
	locked := projectDir(t, claude, "-srv-locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if c := countTurns(t, root, now); c.Unreadable != 1 {
		t.Fatalf("first count Unreadable=%d, want 1", c.Unreadable)
	}
	c := countTurns(t, root, now.Add(time.Hour))
	if c.Age != time.Hour {
		t.Fatalf("second count Age=%v, want a 1h cache hit", c.Age)
	}
	if c.Unreadable != 1 || c.Repo != 1 {
		t.Errorf("a cache hit gave Unreadable=%d Repo=%d, want the saved 1 and 1", c.Unreadable, c.Repo)
	}
}

func TestMemoryStateSelfIgnores(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	claude, root := turnsEnv(t)
	now := time.Now()
	if out, err := exec.Command(git, "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// No root .gitignore at all: a repo onboarded before the seed covered
	// .dross/memory/.
	writeTranscript(t, filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl"), turn(t, "msg_a", now))
	countTurns(t, root, now)

	if _, err := os.Stat(filepath.Join(root, ".gitignore")); err == nil {
		t.Fatal("the fixture repo has a root .gitignore; the self-ignore is not what is being tested")
	}
	if err := exec.Command(git, "-C", root, "check-ignore", "-q", "--", TurnCachePath).Run(); err != nil {
		t.Errorf("git check-ignore does not report %s ignored: %v", TurnCachePath, err)
	}
	out, err := exec.Command(git, "-C", root, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), StateDir) {
		t.Errorf("git status shows advisor state as stageable:\n%s", out)
	}
}
