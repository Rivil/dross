package gatestate

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func bump(t *testing.T, root, session string) {
	t.Helper()
	if err := BumpContext(root, session); err != nil {
		t.Fatalf("BumpContext(%q): %v", session, err)
	}
}

func record(t *testing.T, root, session, kind, sha string) {
	t.Helper()
	if err := RecordShown(root, session, kind, sha); err != nil {
		t.Fatalf("RecordShown(%q, %s): %v", session, kind, err)
	}
}

// TestShowMarkerHappyPath pins the positive arm the negative tests below are
// measured against: a seen session's record in the current epoch is current.
func TestShowMarkerHappyPath(t *testing.T) {
	root := t.TempDir()
	bump(t, root, "s")
	record(t, root, "s", ShowRules, "h")
	if !ShownCurrent(root, "s", ShowRules, "h") {
		t.Fatal("a seen session's current record reads stale")
	}
}

func TestUnseenSessionNeverCurrent(t *testing.T) {
	root := t.TempDir()
	record(t, root, "s", ShowRules, "h")
	if ShownCurrent(root, "s", ShowRules, "h") {
		t.Fatal("a session no SessionStart hook announced reads current")
	}
	// Another session's start makes an epoch exist; s is still unseen.
	bump(t, root, "other")
	record(t, root, "s", ShowRules, "h")
	if ShownCurrent(root, "s", ShowRules, "h") {
		t.Fatal("an unseen session reads current once some epoch exists")
	}
}

func TestAnyBumpInvalidates(t *testing.T) {
	root := t.TempDir()
	bump(t, root, "s1")
	record(t, root, "s1", ShowRules, "h")
	bump(t, root, "s2")
	if ShownCurrent(root, "s1", ShowRules, "h") {
		t.Fatal("s1's record survived another session's start")
	}
	bump(t, root, "s1")
	if ShownCurrent(root, "s1", ShowRules, "h") {
		t.Fatal("s1's record survived its own /clear or compaction")
	}
}

func TestShaMismatch(t *testing.T) {
	root := t.TempDir()
	bump(t, root, "s")
	record(t, root, "s", ShowRules, "h1")
	if ShownCurrent(root, "s", ShowRules, "h2") {
		t.Fatal("a changed digest reads current")
	}
}

func TestKindsIndependent(t *testing.T) {
	root := t.TempDir()
	bump(t, root, "s")
	record(t, root, "s", ShowRules, "h")
	if ShownCurrent(root, "s", ShowPlaybook, "h") {
		t.Fatal("a rules record makes the playbook current")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kind := ShowRules
			if i%2 == 1 {
				kind = ShowPlaybook
			}
			errs <- RecordShown(root, "s", kind, "h")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !ShownCurrent(root, "s", ShowRules, "h") || !ShownCurrent(root, "s", ShowPlaybook, "h") {
		t.Fatal("concurrent records of both kinds left one unreadable")
	}
}

func TestShowKeyContained(t *testing.T) {
	root := t.TempDir()
	for _, sid := range []string{"../../x", "a/b"} {
		bump(t, root, sid)
		record(t, root, sid, ShowRules, "h")
		if !ShownCurrent(root, sid, ShowRules, "h") {
			t.Fatalf("session %q: its own record does not read back", sid)
		}
	}
	// Everything written sits directly in .dross/gate/show/.
	gate := filepath.Join(root, ".dross", "gate")
	show := filepath.Join(gate, ShowDir)
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if dir := filepath.Dir(p); dir != show && !(dir == gate && d.Name() == ".gitignore") {
			t.Errorf("a show write escaped to %s", p)
		}
		return nil
	})
	if ShownCurrent(root, "", ShowRules, "h") {
		t.Fatal("an empty session id reads current")
	}
	if err := RecordShown(root, "", ShowRules, "h"); err == nil {
		t.Fatal("RecordShown accepted an empty session id")
	}
	if err := BumpContext(root, ""); err == nil {
		t.Fatal("BumpContext accepted an empty session id")
	}
	if err := RecordShown(root, "s", "../x", "h"); err == nil {
		t.Fatal("RecordShown accepted a kind that is a path")
	}
}

func TestShowRecordsIgnored(t *testing.T) {
	dir := repo(t)
	bump(t, dir, "s")
	record(t, dir, "s", ShowRules, "h")
	if st := git(t, dir, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("git status after a show record:\n%s", st)
	}
}

func TestShowPrune(t *testing.T) {
	root := t.TempDir()
	bump(t, root, "old")
	record(t, root, "old", ShowRules, "h")
	if !claim(t, root, "s", 1) {
		t.Fatal("seed claim lost")
	}
	if err := SaveGreen(root, Green{Tree: "abc", At: at, Runner: "local"}); err != nil {
		t.Fatal(err)
	}
	show := showPath(root)
	oldRecord := filepath.Join(show, nudgeKey("old")+"-"+ShowRules)
	oldSeen := filepath.Join(show, nudgeKey("old")+showSeenExt)
	claimFile := filepath.Join(root, ".dross", "gate", NudgeDir, nudgeKey("s")+"-1")
	green := Path(root, GreenFile)
	for _, p := range []string{oldRecord, oldSeen, claimFile, green} {
		past := time.Now().Add(-8 * 24 * time.Hour)
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}

	bump(t, root, "new")
	for _, p := range []string{oldRecord, oldSeen} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("an 8-day-old %s survived the prune (stat err = %v)", filepath.Base(p), err)
		}
	}
	for _, p := range []string{claimFile, green, filepath.Join(show, showEpochFile)} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("the prune removed %s: %v", p, err)
		}
	}
}

func TestShownCurrentReadOnly(t *testing.T) {
	root := t.TempDir()
	if ShownCurrent(root, "s", ShowRules, "h") {
		t.Fatal("a fresh root reads current")
	}
	if _, err := os.Stat(filepath.Join(root, ".dross")); !os.IsNotExist(err) {
		t.Fatalf("ShownCurrent created .dross/ (stat err = %v)", err)
	}
}

func TestBumpForHook(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"not json", `["a"]`, `{"source":"clear"}`, `{"session_id":7}`} {
		if err := BumpForHook(root, []byte(bad)); err == nil {
			t.Errorf("BumpForHook(%q) accepted a payload naming no session", bad)
		}
	}
	if _, err := os.Stat(showPath(root)); !os.IsNotExist(err) {
		t.Fatalf("a rejected payload wrote show records (stat err = %v)", err)
	}
	if err := BumpForHook(root, []byte(`{"session_id":"s","source":"compact","extra":{"k":1}}`)); err != nil {
		t.Fatal(err)
	}
	record(t, root, "s", ShowRules, "h")
	if !ShownCurrent(root, "s", ShowRules, "h") {
		t.Fatal("the payload's session was not marked seen")
	}
}
