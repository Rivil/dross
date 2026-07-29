package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var pauseNow = time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

// pauseRepo scaffolds a dross repo in a temp dir and chdirs into it.
func pauseRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPauseAutoPreservesHandwritten(t *testing.T) {
	dir := pauseRepo(t)
	handoff := filepath.Join(dir, ".dross", "handoff.md")

	thread := "# Handoff — paused 2026-07-22 09:00\n\n## Thread\nDeep in the merge-helper design; ruled out sed.\n\n"
	loops := "## Open loops\n- [ ] revisit the exempt list\n"
	stale := "## Auto-snapshot\n- captured: 2026-07-20 08:00 UTC\n- branch: old\n\n"
	mustWrite(t, handoff, thread+stale+loops)

	if err := pauseAuto(pauseNow); err != nil {
		t.Fatalf("pauseAuto: %v", err)
	}

	got := mustRead(t, handoff)
	if !strings.HasPrefix(got, thread) {
		t.Errorf("hand-written prefix (header + Thread) not byte-identical:\n%s", got)
	}
	if !strings.HasSuffix(got, loops) {
		t.Errorf("hand-written suffix (Open loops) not byte-identical:\n%s", got)
	}
	if strings.Contains(got, "2026-07-20 08:00 UTC") {
		t.Error("stale auto-snapshot content survived the rewrite")
	}
	if !strings.Contains(got, "- captured: 2026-07-23 12:00 UTC") {
		t.Errorf("fresh snapshot missing:\n%s", got)
	}
}

func TestPauseAutoIdempotentMerge(t *testing.T) {
	dir := pauseRepo(t)
	handoff := filepath.Join(dir, ".dross", "handoff.md")
	mustWrite(t, handoff, "## Thread\nmid-task\n\n## Auto-snapshot\n- captured: old\n\n## Open loops\n- [ ] item\n")

	if err := pauseAuto(pauseNow); err != nil {
		t.Fatalf("first: %v", err)
	}
	first := mustRead(t, handoff)
	if err := pauseAuto(pauseNow); err != nil {
		t.Fatalf("second: %v", err)
	}
	second := mustRead(t, handoff)

	if first != second {
		t.Errorf("second merge not byte-identical:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if n := strings.Count(second, "## Auto-snapshot"); n != 1 {
		t.Errorf("want exactly 1 auto-snapshot section, got %d", n)
	}
	// The heading after the auto section must not be consumed by the merge.
	if !strings.Contains(second, "\n## Open loops\n- [ ] item\n") {
		t.Errorf("following section consumed:\n%s", second)
	}
}

func TestPauseAutoCreatesFileWhenMissing(t *testing.T) {
	dir := pauseRepo(t)

	if err := pauseAuto(pauseNow); err != nil {
		t.Fatalf("pauseAuto: %v", err)
	}
	got := mustRead(t, filepath.Join(dir, ".dross", "handoff.md"))
	if !strings.HasPrefix(got, "## Auto-snapshot\n") {
		t.Errorf("fresh handoff should be just the snapshot section:\n%s", got)
	}
}

// promptTripwire fails the test if anything reads from it.
type promptTripwire struct{ t *testing.T }

func (r *promptTripwire) Read([]byte) (int, error) {
	r.t.Error("pause --auto read stdin — it must never prompt")
	return 0, os.ErrClosed
}

func TestPauseAutoNoPrompt(t *testing.T) {
	pauseRepo(t)

	c := Pause()
	c.SetIn(&promptTripwire{t: t})
	c.SetArgs([]string{"--auto"})
	c.SetOut(new(bytes.Buffer))
	c.SetErr(new(bytes.Buffer))

	// Also guard the fmt.Print* path: handlers write via os.Stdin only if they
	// prompt; swapping os.Stdin for a closed pipe makes any hidden read fail.
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() { os.Stdin = origStdin; _ = r.Close() }()

	if err := c.Execute(); err != nil {
		t.Fatalf("pause --auto: %v", err)
	}
}

func TestPauseAutoNoopOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := runCmd(t, Pause(), "--auto"); err != nil {
		t.Fatalf("want silent exit 0 outside a dross repo, got: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("no files should be created outside a dross repo, found: %v", entries)
	}
}

func TestPauseWithoutAutoErrors(t *testing.T) {
	pauseRepo(t)
	if err := runCmd(t, Pause()); err == nil {
		t.Fatal("bare `dross pause` should refuse — only --auto is implemented")
	}
}

func TestPauseAutoSnapshotCapturesGitState(t *testing.T) {
	dir := pauseRepo(t)
	gitInit(t, dir, "https://forge.example/me/p.git")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-qm", "baseline")
	mustGit(t, dir, "checkout", "-qb", "phase/pause-test")
	for _, name := range []string{"d1.txt", "d2.txt", "d3.txt", "d4.txt", "d5.txt", "d6.txt", "d7.txt"} {
		mustWrite(t, filepath.Join(dir, name), "x")
	}

	if err := pauseAuto(pauseNow); err != nil {
		t.Fatalf("pauseAuto: %v", err)
	}
	handoff := filepath.Join(dir, ".dross", "handoff.md")
	got := mustRead(t, handoff)
	if !strings.Contains(got, "- branch: phase/pause-test\n") {
		t.Errorf("branch line missing or wrong:\n%s", got)
	}
	if !strings.Contains(got, "- dirty: 7 file(s): d1.txt, d2.txt, d3.txt, d4.txt, d5.txt +2 more\n") {
		t.Errorf("dirty line should truncate past 5 paths:\n%s", got)
	}

	// Exactly 5 dirty entries must list every path with no "+N more" tail.
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-qm", "checkpoint")
	for _, name := range []string{"e1.txt", "e2.txt", "e3.txt", "e4.txt", "e5.txt"} {
		mustWrite(t, filepath.Join(dir, name), "x")
	}
	if err := pauseAuto(pauseNow); err != nil {
		t.Fatalf("second pauseAuto: %v", err)
	}
	got = mustRead(t, handoff)
	if !strings.Contains(got, "- dirty: 5 file(s): e1.txt, e2.txt, e3.txt, e4.txt, e5.txt\n") {
		t.Errorf("five-entry dirty line should list all paths without a more-tail:\n%s", got)
	}
}

func TestPauseAutoSnapshotCleanRepo(t *testing.T) {
	dir := pauseRepo(t)
	gitInit(t, dir, "https://forge.example/me/p.git")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-qm", "baseline")

	if err := pauseAuto(pauseNow); err != nil {
		t.Fatalf("pauseAuto: %v", err)
	}
	got := mustRead(t, filepath.Join(dir, ".dross", "handoff.md"))
	if !strings.Contains(got, "- branch: main\n") {
		t.Errorf("branch line should name main:\n%s", got)
	}
	if !strings.Contains(got, "- dirty: clean\n") {
		t.Errorf("clean repo should render '- dirty: clean':\n%s", got)
	}
}

// TestPauseAutoSilentOnIncompleteRoot (c-2): the PreCompact hook fires
// everywhere, so an incomplete `.dross/` is a silent exit 0. The real
// assertion is the file-existence half — a fix that returns nil after writing
// the snapshot passes an error check and fails here.
func TestPauseAutoSilentOnIncompleteRoot(t *testing.T) {
	dir := realTempDir(t)
	root := mkRoot(t, dir, "project.toml")
	chdir(t, dir)

	if err := pauseAuto(pauseNow); err != nil {
		t.Fatalf("pause --auto should exit 0 on an incomplete root, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "handoff.md")); !os.IsNotExist(err) {
		t.Errorf("pause --auto must not write handoff.md on an incomplete root; stat err = %v", err)
	}
}

// TestPauseAutoLoudOnCorruptFile (locked completeness_check): a file that
// exists but won't decode is broken state, loud even in the hook target. Each
// file is its own row, so fixing only the state path leaves a named failure.
// Every row also asserts the write never happened — an error returned *after*
// clobbering handoff.md would be a worse bug than the silence it replaces.
func TestPauseAutoLoudOnCorruptFile(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
	}{
		{"truncated state.json", "state.json", `{"version":`},
		{"undecodable project.toml", "project.toml", "[[[not toml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := realTempDir(t)
			root := mkRoot(t, dir, "project.toml")
			if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, tc.file), []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			// A handoff the user already wrote must survive the failed run.
			handoff := filepath.Join(root, "handoff.md")
			const prior = "# Handoff\n\nhand-written thread\n"
			if err := os.WriteFile(handoff, []byte(prior), 0o644); err != nil {
				t.Fatal(err)
			}
			chdir(t, dir)

			err := pauseAuto(pauseNow)
			if err == nil {
				t.Fatalf("pause --auto should fail on a corrupt %s", tc.file)
			}
			if !strings.Contains(err.Error(), tc.file) {
				t.Errorf("error should name %s, got %v", tc.file, err)
			}
			got, readErr := os.ReadFile(handoff)
			if readErr != nil {
				t.Fatalf("pre-existing handoff.md disappeared: %v", readErr)
			}
			if string(got) != prior {
				t.Errorf("handoff.md was modified by a failed run:\n%s", got)
			}
		})
	}
}

// TestPauseAutoDegradesWithoutGit pins the other side of the loud/soft split:
// a missing git repo is an environment fact, not broken state, so the snapshot
// still renders and the hook still exits 0.
func TestPauseAutoDegradesWithoutGit(t *testing.T) {
	dir := pauseRepo(t) // dross-initialised, never git-initialised

	if err := pauseAuto(pauseNow); err != nil {
		t.Fatalf("pause --auto should degrade past a missing git repo, got %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".dross", "handoff.md"))
	if err != nil {
		t.Fatalf("handoff.md should have been written: %v", err)
	}
	if !strings.Contains(string(b), "- branch: (no git)") {
		t.Errorf("snapshot should degrade the branch line to (no git):\n%s", b)
	}
}
