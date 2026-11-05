package gatestate

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func claim(t *testing.T, root, session string, band int64) bool {
	t.Helper()
	won, err := ClaimNudge(root, session, band)
	if err != nil {
		t.Fatalf("ClaimNudge(%q, %d): %v", session, band, err)
	}
	return won
}

// TestClaimNudgeExactlyOnce: concurrent hook fires for one band nudge once.
// A stat-then-write claim lets several through; the O_EXCL create does not.
func TestClaimNudgeExactlyOnce(t *testing.T) {
	root := t.TempDir()
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if won, err := ClaimNudge(root, "s", 1); err == nil && won {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := wins.Load(); n != 1 {
		t.Errorf("%d of 32 concurrent claims won band 1; want exactly 1", n)
	}
}

// TestLowerBandNeverRenudges: a session's nudges only climb. Once band 3 is
// claimed — directly, by a jump from below the threshold — bands 1 and 2
// can never fire, and band 3 fires once.
func TestLowerBandNeverRenudges(t *testing.T) {
	root := t.TempDir()
	if !claim(t, root, "s", 3) {
		t.Fatal("first claim of band 3 lost")
	}
	for _, b := range []int64{1, 2, 3} {
		if claim(t, root, "s", b) {
			t.Errorf("band %d claimed after band 3", b)
		}
	}
	if !claim(t, root, "s", 4) {
		t.Error("band 4 lost after band 3 — the next band must still fire")
	}
}

// TestClaimsArePerSession: one session's claim never silences another's.
func TestClaimsArePerSession(t *testing.T) {
	root := t.TempDir()
	for _, c := range []struct {
		session string
		band    int64
		want    bool
	}{
		{"s1", 1, true},
		{"s2", 1, true},
		{"s1", 1, false},
		{"s1", 2, true},
	} {
		if got := claim(t, root, c.session, c.band); got != c.want {
			t.Errorf("ClaimNudge(%s, %d) = %v, want %v", c.session, c.band, got, c.want)
		}
	}
}

// TestClaimPathStaysInside: the session id is hashed into the file name, so no
// id can place a claim outside .dross/gate/nudge/; an empty id is refused.
func TestClaimPathStaysInside(t *testing.T) {
	root := t.TempDir()
	for _, s := range []string{"../../escape", "a/b"} {
		if !claim(t, root, s, 1) {
			t.Fatalf("claim for %q lost", s)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, ".dross", "gate", "nudge"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("nudge dir holds %d entries, want the 2 claims", len(entries))
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); !os.IsNotExist(err) {
		t.Errorf("a claim escaped the nudge dir: stat err = %v", err)
	}
	var outside []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.Contains(p, filepath.Join(".dross", "gate")) {
			outside = append(outside, p)
		}
		return nil
	})
	if len(outside) != 0 {
		t.Errorf("files written outside .dross/gate: %v", outside)
	}

	empty := t.TempDir()
	if won, err := ClaimNudge(empty, "", 1); err == nil || won {
		t.Errorf("empty session id: (%v, %v), want (false, error)", won, err)
	}
	if _, err := os.Stat(filepath.Join(empty, ".dross")); !os.IsNotExist(err) {
		t.Errorf("an empty-session claim created .dross (stat err = %v)", err)
	}
}

// TestNudgeClaimsIgnored: a claim never dirties the work tree.
func TestNudgeClaimsIgnored(t *testing.T) {
	dir := repo(t)
	if !claim(t, dir, "s", 1) {
		t.Fatal("claim lost")
	}
	if st := git(t, dir, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("git status after a claim:\n%s", st)
	}
}

// TestNudgeClaimPrune: a successful claim drops claims older than seven days
// from the nudge dir — any session's — and touches nothing else.
func TestNudgeClaimPrune(t *testing.T) {
	root := t.TempDir()
	if !claim(t, root, "old", 1) || !claim(t, root, "recent", 1) {
		t.Fatal("seed claims lost")
	}
	if err := SaveGreen(root, Green{Tree: "abc", At: at, Runner: "local"}); err != nil {
		t.Fatal(err)
	}
	nudge := filepath.Join(root, ".dross", "gate", "nudge")
	oldClaim := filepath.Join(nudge, nudgeKey("old")+"-1")
	recentClaim := filepath.Join(nudge, nudgeKey("recent")+"-1")
	green := Path(root, GreenFile)
	age := func(p string, d time.Duration) {
		if err := os.Chtimes(p, time.Now().Add(-d), time.Now().Add(-d)); err != nil {
			t.Fatal(err)
		}
	}
	age(oldClaim, 8*24*time.Hour)
	age(recentClaim, 24*time.Hour)
	age(green, 8*24*time.Hour)

	if !claim(t, root, "new", 1) {
		t.Fatal("pruning claim lost")
	}
	if _, err := os.Stat(oldClaim); !os.IsNotExist(err) {
		t.Errorf("an 8-day-old claim survived the prune (stat err = %v)", err)
	}
	if _, err := os.Stat(recentClaim); err != nil {
		t.Errorf("a 1-day-old claim of another session was pruned: %v", err)
	}
	if _, err := os.Stat(green); err != nil {
		t.Errorf("the prune reached outside nudge/ and removed %s: %v", GreenFile, err)
	}
}

// TestNudgeClaimedIsReadOnly: the peek the hook runs on every fire never
// creates anything, and reads the high-water mark.
func TestNudgeClaimedIsReadOnly(t *testing.T) {
	root := t.TempDir()
	if NudgeClaimed(root, "s", 1) {
		t.Error("fresh root reports band 1 claimed")
	}
	if _, err := os.Stat(filepath.Join(root, ".dross", "gate", "nudge")); !os.IsNotExist(err) {
		t.Errorf("NudgeClaimed created the nudge dir (stat err = %v)", err)
	}
	if !claim(t, root, "s", 3) {
		t.Fatal("claim lost")
	}
	for _, c := range []struct {
		band int64
		want bool
	}{{2, true}, {3, true}, {4, false}} {
		if got := NudgeClaimed(root, "s", c.band); got != c.want {
			t.Errorf("NudgeClaimed(s, %d) = %v, want %v", c.band, got, c.want)
		}
	}
	if NudgeClaimed(root, "other", 1) {
		t.Error("another session reads s's claim")
	}
}
