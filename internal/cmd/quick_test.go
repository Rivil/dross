package cmd

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/gatestate"
)

// `dross quick end` absorbed /dross-quick's old §6: after the work commit it
// bumps internal, records the quick in an unshipped phase, touches state and
// opens the board issue. These tests drive it the way the prompt does — begin,
// commit (or not), end — and read the files it writes.

// quickEndFixture is soloBeginFixture at version 1.2.3.4, committed clean.
func quickEndFixture(t *testing.T) string {
	t.Helper()
	dir := soloBeginFixture(t)
	if err := runCmd(t, State(), "set", "version", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "version")
	return dir
}

// setCurrentPhase points state.json at phase with status (empty leaves the
// status unset).
func setQuickPhase(t *testing.T, phase, status string) {
	t.Helper()
	if err := runCmd(t, State(), "set", "current_phase", phase); err != nil {
		t.Fatal(err)
	}
	if status != "" {
		if err := runCmd(t, State(), "set", "current_phase_status", status); err != nil {
			t.Fatal(err)
		}
	}
}

func beginQuick(t *testing.T, desc string) {
	t.Helper()
	if err := runCmd(t, Quick(), "begin", desc); err != nil {
		t.Fatal(err)
	}
}

// quickWork commits an edit to a.go, plus extra paths force-added (gitignored
// .dross files among them), as the quick's work commit; it returns HEAD.
func quickWork(t *testing.T, dir string, extra ...string) string {
	t.Helper()
	mustWrite(t, filepath.Join(dir, "a.go"), "package a\n\nvar X = 1\n")
	mustGit(t, dir, append([]string{"add", "-f", "a.go"}, extra...)...)
	mustGit(t, dir, "commit", "-q", "-m", "quick: x")
	return mustGit(t, dir, "rev-parse", "HEAD")
}

func endQuick(t *testing.T) (string, error) {
	t.Helper()
	var out string
	err := runCmdCapturing(t, &out, Quick(), "end")
	return out, err
}

func changesPathAt(dir, phase string) string {
	return changes.FilePath(filepath.Join(dir, ".dross"), phase)
}

func loadChangesAt(t *testing.T, dir, phase string) *changes.Changes {
	t.Helper()
	c, err := changes.Load(changesPathAt(dir, phase), phase)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestQuickEndAbortNoBump(t *testing.T) {
	dir := quickEndFixture(t)
	setQuickPhase(t, "p", "")
	hits := countingBoard(t, failingBoard)
	beginQuick(t, "fix x")
	// The abort discarded its edit: nothing was committed since begin.
	out, err := endQuick(t)
	if err != nil {
		t.Fatalf("quick end after an abort: %v\n%s", err, out)
	}
	assertVersionParity(t, dir, "1.2.3.4")
	if _, err := os.Stat(changesPathAt(dir, "p")); !os.IsNotExist(err) {
		t.Errorf("an abort wrote changes.json (stat err %v)", err)
	}
	if *hits != 0 {
		t.Errorf("an abort called the board %d times, want none", *hits)
	}
	if q := loadQuick(t, dir); q != nil {
		t.Errorf("an abort left the quick record: %+v", q)
	}
	if strings.Contains(out, "version:") || !strings.Contains(out, "nothing bumped") {
		t.Errorf("abort output %q, want no version line and `nothing bumped`", out)
	}
}

// TestQuickEndUnreadableRecord: a corrupt record still clears, as an abort
// path needs, but end says so and bumps nothing — even after a work commit
// that a readable record would have bumped for.
func TestQuickEndUnreadableRecord(t *testing.T) {
	dir := quickEndFixture(t)
	setQuickPhase(t, "p", "")
	hits := countingBoard(t, failingBoard)
	beginQuick(t, "fix x")
	quickWork(t, dir)
	recPath := gatestate.Path(dir, gatestate.QuickFile)
	if err := os.WriteFile(recPath, []byte(`{"mode":"pa`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := endQuick(t)
	if err == nil || !strings.Contains(err.Error(), "quick record unreadable, removed it; nothing was bumped") {
		t.Fatalf("quick end on an unreadable record = %v (out %q), want the unreadable-record error", err, out)
	}
	if _, err := os.Stat(recPath); !os.IsNotExist(err) {
		t.Errorf("quick end left the unreadable record (stat err %v)", err)
	}
	assertVersionParity(t, dir, "1.2.3.4")
	if _, err := os.Stat(changesPathAt(dir, "p")); !os.IsNotExist(err) {
		t.Errorf("an unreadable record wrote changes.json (stat err %v)", err)
	}
	if *hits != 0 {
		t.Errorf("an unreadable record called the board %d times, want none", *hits)
	}
}

// TestQuickEndNoRecord: with no record there is no begin to measure against,
// so even a moved HEAD bumps nothing and the end writes nothing.
func TestQuickEndNoRecord(t *testing.T) {
	dir := quickEndFixture(t)
	setQuickPhase(t, "p", "")
	hits := countingBoard(t, failingBoard)
	quickWork(t, dir)
	before := treeSnapshot(t, dir)
	out, err := endQuick(t)
	if err != nil {
		t.Fatalf("quick end with no record: %v", err)
	}
	if after := treeSnapshot(t, dir); after != before {
		t.Errorf("quick end with no record wrote to the tree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if *hits != 0 || strings.Contains(out, "version:") {
		t.Errorf("quick end with no record called the board %d times / printed %q", *hits, out)
	}
}

func TestQuickEndRecordsInPhase(t *testing.T) {
	dir := quickEndFixture(t)
	setQuickPhase(t, "p", "executing")
	beginQuick(t, "  fix x\nwith more detail")
	head := quickWork(t, dir, ".dross/state.json")
	out, err := endQuick(t)
	if err != nil {
		t.Fatalf("quick end: %v\n%s", err, out)
	}
	if !strings.Contains(out, "\nversion: 1.2.3.5\n") {
		t.Errorf("output %q lacks the `version: 1.2.3.5` line the prompt reads", out)
	}
	assertVersionParity(t, dir, "1.2.3.5")
	rec, ok := loadChangesAt(t, dir, "p").Tasks["quick-5"]
	if !ok {
		t.Fatalf("no quick-5 record in changes.json; output:\n%s", out)
	}
	// The commit touched .dross/state.json too; only the work is recorded.
	if !reflect.DeepEqual(rec.Files, []string{"a.go"}) {
		t.Errorf("recorded files %v, want [a.go] — .dross/ is bookkeeping, not the quick's work", rec.Files)
	}
	if rec.Commit != head[:7] || rec.Notes != "quick:   fix x\nwith more detail" {
		t.Errorf("recorded commit %q notes %q, want %q and the description", rec.Commit, rec.Notes, head[:7])
	}
	h := loadStateAt(t, dir).History
	if n := len(h); n < 2 || h[n-2].Action != "bump internal 1.2.3.4 → 1.2.3.5" || h[n-1].Action != "quick: fix x" {
		t.Errorf("state history ends %+v, want the bump then `quick: fix x`", h)
	}
	if q := loadQuick(t, dir); q != nil {
		t.Errorf("quick end left the record: %+v", q)
	}
	if !strings.Contains(out, "board: off") {
		t.Errorf("output %q, want `board: off` with board sync disabled", out)
	}
}

func TestQuickEndIdempotent(t *testing.T) {
	dir := quickEndFixture(t)
	beginQuick(t, "fix x")
	quickWork(t, dir)
	for i := 0; i < 2; i++ {
		if out, err := endQuick(t); err != nil {
			t.Fatalf("end #%d: %v\n%s", i+1, err, out)
		}
	}
	assertVersionParity(t, dir, "1.2.3.5")
}

// TestQuickEndMatchesPRBranch: the PR route works on quick/<preview-version>,
// begin's version with internal+1, and the bump must land on that name.
func TestQuickEndMatchesPRBranch(t *testing.T) {
	dir := quickEndFixture(t)
	mustGit(t, dir, "checkout", "-q", "-b", "quick/1.2.3.5")
	beginQuick(t, "fix x")
	quickWork(t, dir)
	out, err := endQuick(t)
	if err != nil {
		t.Fatalf("quick end: %v\n%s", err, out)
	}
	branch := mustGit(t, dir, "symbolic-ref", "--short", "HEAD")
	if want := "version: " + strings.TrimPrefix(branch, "quick/") + "\n"; !strings.Contains(out, want) {
		t.Errorf("output %q, want %q to match the branch %s", out, want, branch)
	}
	if !strings.Contains(out, "changes: none (standalone)") {
		t.Errorf("a standalone quick reported %q, want no changes record", out)
	}
}

func TestQuickEndShippedIsStandalone(t *testing.T) {
	dir := quickEndFixture(t)
	setQuickPhase(t, "p", "shipped")
	beginQuick(t, "fix x")
	quickWork(t, dir)
	out, err := endQuick(t)
	if err != nil {
		t.Fatalf("quick end: %v\n%s", err, out)
	}
	assertVersionParity(t, dir, "1.2.3.5")
	if _, err := os.Stat(changesPathAt(dir, "p")); !os.IsNotExist(err) {
		t.Errorf("a quick on a shipped phase wrote its changes.json (stat err %v)", err)
	}
	if !strings.Contains(out, "phase p is shipped") {
		t.Errorf("output %q does not say the shipped phase made it standalone", out)
	}
}

func TestQuickEndBoardFailureKeepsBump(t *testing.T) {
	dir := quickEndFixture(t)
	setQuickPhase(t, "p", "executing")
	hits := countingBoard(t, failingBoard)
	beginQuick(t, "fix it's x")
	quickWork(t, dir)
	out, err := endQuick(t)
	if err == nil {
		t.Fatalf("quick end succeeded against a failing board:\n%s", out)
	}
	if *hits == 0 {
		t.Error("the board was never called")
	}
	usage := runCmd(t, Quick(), "end", "extra")
	if code := ExitCode(err); code != exitQuickBoardFailed || code == ExitCode(usage) {
		t.Errorf("exit code %d, want %d — distinct from a usage error's %d", code, exitQuickBoardFailed, ExitCode(usage))
	}
	for _, want := range []string{"version: 1.2.3.5\n", "board: FAILED", `dross issue quick 1.2.3.5 'quick: fix it'\''s x'`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	assertVersionParity(t, dir, "1.2.3.5")
	if _, ok := loadChangesAt(t, dir, "p").Tasks["quick-5"]; !ok {
		t.Error("the board failure lost the changes record")
	}
	if q := loadQuick(t, dir); q != nil {
		t.Errorf("the board failure left the quick record, so a retry would bump again: %+v", q)
	}
}

func TestQuickEndOpensBoardIssue(t *testing.T) {
	dir := quickEndFixture(t)
	f := newYTPhaseFake()
	countingBoard(t, f.handler(t))
	beginQuick(t, "fix x")
	quickWork(t, dir)
	out, err := endQuick(t)
	if err != nil {
		t.Fatalf("quick end: %v\n%s", err, out)
	}
	if !strings.Contains(out, "board: quick 1.2.3.5 -> board PROJ-100") {
		t.Errorf("output %q, want the board line", out)
	}
	b, err := board.Load(filepath.Join(dir, ".dross", board.File))
	if err != nil {
		t.Fatal(err)
	}
	if key, ok := b.QuickIssue("1.2.3.5"); !ok || key != "PROJ-100" {
		t.Errorf("board.json links quick 1.2.3.5 to %q (%t), want PROJ-100 — the close finds it there", key, ok)
	}
	if f.creates != 1 || !slicesHas(f.tags["PROJ-100"], "dross/quick") {
		t.Errorf("issue tags %v, want the quick label", f.tags["PROJ-100"])
	}
}

// boardWrites runs a fresh YouTrack fake as the fixture's board and returns
// a log of every write it receives, as "METHOD path body".
func boardWrites(t *testing.T) *[]string {
	t.Helper()
	f := newYTPhaseFake()
	var mu sync.Mutex
	var writes []string
	h := f.handler(t)
	countingBoard(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			r.Body = io.NopCloser(bytes.NewReader(b))
			mu.Lock()
			writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
			mu.Unlock()
		}
		h(w, r)
	})
	return &writes
}

// quickIssueLink is the board issue board.json links quick ref to.
func quickIssueLink(t *testing.T, dir, ref string) string {
	t.Helper()
	b, err := board.Load(filepath.Join(dir, ".dross", board.File))
	if err != nil {
		t.Fatal(err)
	}
	key, _ := b.QuickIssue(ref)
	return key
}

// `dross quick end` and `dross issue quick` open the quick issue through one
// helper: the same ref and title against the same board send the same writes
// (title, body, labels) and link the same key in board.json.
func TestQuickEndAndIssueQuickOpenTheSameIssue(t *testing.T) {
	// quick end titles the issue "quick: <summary>" of its begin description;
	// the CLI is handed that same title.
	const ref, title = "1.2.3.5", "quick: fix x"
	var endLink, cliLink string
	var endWrites, cliWrites []string
	t.Run("quick end", func(t *testing.T) {
		dir := quickEndFixture(t)
		writes := boardWrites(t)
		beginQuick(t, "fix x")
		quickWork(t, dir)
		if out, err := endQuick(t); err != nil {
			t.Fatalf("quick end: %v\n%s", err, out)
		}
		endWrites, endLink = *writes, quickIssueLink(t, dir, ref)
	})
	t.Run("issue quick", func(t *testing.T) {
		dir := quickEndFixture(t)
		writes := boardWrites(t)
		if err := runCmd(t, Issue(), "quick", ref, title); err != nil {
			t.Fatal(err)
		}
		cliWrites, cliLink = *writes, quickIssueLink(t, dir, ref)
	})
	// The writes carry the title, body and labels, so comparing them compares
	// the issues.
	sent := strings.Join(endWrites, "\n")
	for _, want := range []string{title, "Quick task `" + ref + "`", "dross/quick"} {
		if !strings.Contains(sent, want) {
			t.Fatalf("quick end's board writes lack %q:\n%s", want, sent)
		}
	}
	if !reflect.DeepEqual(endWrites, cliWrites) {
		t.Errorf("the two paths diverged on the board:\nquick end:   %q\nissue quick: %q", endWrites, cliWrites)
	}
	if endLink != "PROJ-100" || cliLink != endLink {
		t.Errorf("board.json links quick %s to %q (quick end) and %q (issue quick), want PROJ-100 both", ref, endLink, cliLink)
	}
}

func TestQuickSummary(t *testing.T) {
	for in, want := range map[string]string{
		"fix x":               "fix x",
		"  fix x  \nmore":     "fix x",
		"\n\n  fix x\nmore\n": "fix x",
	} {
		if got := quickSummary(in); got != want {
			t.Errorf("quickSummary(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQuickVersion(t *testing.T) {
	for in, want := range map[string]struct {
		next     string
		internal int
	}{
		"1.2.3.4":   {"1.2.3.5", 5},
		"0.9.10.99": {"0.9.10.100", 100},
		"01.2.3.09": {"1.2.3.10", 10},
	} {
		next, internal, err := quickVersion(in)
		if err != nil || next != want.next || internal != want.internal {
			t.Errorf("quickVersion(%q) = %q, %d, %v; want %q, %d", in, next, internal, err, want.next, want.internal)
		}
	}
	for _, bad := range []string{"1.2.3", "1.2.3.x", "1.2.3.-1", ""} {
		if _, _, err := quickVersion(bad); err == nil {
			t.Errorf("quickVersion(%q) accepted a version that is not 4 non-negative numbers", bad)
		}
	}
}
