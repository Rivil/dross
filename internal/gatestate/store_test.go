package gatestate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/review"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// repo is a git repo with a committed .dross/project.toml.
func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "T")
	git(t, dir, "config", "commit.gpgsign", "false")
	if err := os.MkdirAll(filepath.Join(dir, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dross", "project.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".dross")
	git(t, dir, "commit", "-q", "-m", "init")
	return dir
}

var at = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

// TestGateDirUntracked: the directory ignores itself. No root .gitignore line
// is needed, and nothing in it ever shows or stages.
func TestGateDirUntracked(t *testing.T) {
	dir := repo(t)
	if err := SaveGreen(dir, Green{Tree: "abc", At: at, Runner: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveApproval(dir, Approval{Phase: "p", Task: "t-1", Head: "h", At: at}); err != nil {
		t.Fatal(err)
	}
	if st := git(t, dir, "status", "--porcelain", "--untracked-files=all"); strings.Contains(st, ".dross/gate") {
		t.Errorf("git status shows the gate dir:\n%s", st)
	}
	git(t, dir, "add", ".dross/")
	if staged := git(t, dir, "diff", "--cached", "--name-only"); strings.Contains(staged, ".dross/gate") {
		t.Errorf("git add .dross/ staged gate records:\n%s", staged)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("a root .gitignore appeared (err=%v); the gate dir must ignore itself", err)
	}
}

// TestConcurrentWritesAreAtomic: writers racing readers. A reader only ever
// sees no record or a whole one — never a torn or truncated file.
func TestConcurrentWritesAreAtomic(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 1000)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				tree := strings.Repeat(string(rune('a'+w)), 200+i)
				if err := SaveGreen(dir, Green{Tree: tree, At: at, Runner: "local"}); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				g, err := LoadGreen(dir)
				if err != nil {
					errs <- err
					continue
				}
				if g != nil && len(g.Tree) < 200 {
					errs <- os.ErrInvalid
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("a reader saw a torn record: %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".dross", "gate", ".*.tmp"))
	if len(left) != 0 {
		t.Errorf("temp files left behind: %q", left)
	}
}

func TestRecords(t *testing.T) {
	dir := t.TempDir()
	if g, err := LoadGreen(dir); g != nil || err != nil {
		t.Errorf("missing green = %+v, %v; want nil, nil", g, err)
	}
	if e, err := LoadExecute(dir); e != nil || err != nil {
		t.Errorf("missing execute = %+v, %v; want nil, nil", e, err)
	}
	if a, err := LoadApproval(dir); a != nil || err != nil {
		t.Errorf("missing approval = %+v, %v; want nil, nil", a, err)
	}

	want := Green{Tree: "4b825dc", At: at, Runner: "helicon"}
	if err := SaveGreen(dir, want); err != nil {
		t.Fatal(err)
	}
	if g, err := LoadGreen(dir); err != nil || *g != want {
		t.Errorf("green round trip = %+v, %v", g, err)
	}
	if err := SaveExecute(dir, Execute{Phase: "p", Mode: "solo", At: at}); err != nil {
		t.Fatal(err)
	}
	if e, err := LoadExecute(dir); err != nil || e.Mode != "solo" || e.Phase != "p" {
		t.Errorf("execute round trip = %+v, %v", e, err)
	}
	if err := SaveApproval(dir, Approval{Phase: "p", Task: "t-3", Head: "deadbeef", At: at}); err != nil {
		t.Fatal(err)
	}
	if a, err := LoadApproval(dir); err != nil || a.Task != "t-3" || a.Head != "deadbeef" {
		t.Errorf("approval round trip = %+v, %v", a, err)
	}

	if err := SaveGreen(dir, Green{Tree: " ", At: at}); err == nil {
		t.Error("a green with no tree was saved")
	}
	if err := os.WriteFile(Path(dir, GreenFile), []byte(`{"tree":"","at":"2026-10-03T09:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if g, err := LoadGreen(dir); err == nil || g != nil {
		t.Errorf("a green with an empty tree on disk = %+v, %v; want an error", g, err)
	}
	if err := os.WriteFile(Path(dir, GreenFile), []byte(`{"tree":"abc","at":"2026-`), 0o644); err != nil {
		t.Fatal(err)
	}
	if g, err := LoadGreen(dir); err == nil || g != nil || !strings.Contains(err.Error(), ".dross/gate/green.json") {
		t.Errorf("a truncated green = %+v, %v; want an error naming .dross/gate/green.json", g, err)
	}

	if err := ClearGreen(dir); err != nil {
		t.Fatal(err)
	}
	if err := ClearGreen(dir); err != nil {
		t.Errorf("clearing an absent green: %v", err)
	}
	if g, err := LoadGreen(dir); g != nil || err != nil {
		t.Errorf("after ClearGreen = %+v, %v", g, err)
	}
}

// TestTrackedRecordRefused: a record force-added to git is refused unread.
func TestTrackedRecordRefused(t *testing.T) {
	dir := repo(t)
	if err := SaveGreen(dir, Green{Tree: "abc", At: at}); err != nil {
		t.Fatal(err)
	}
	if err := SaveApproval(dir, Approval{Phase: "p", Task: "t-1", Head: "h", At: at}); err != nil {
		t.Fatal(err)
	}
	if err := SaveWriter(dir, sampleWriter()); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-f", ".dross/gate/green.json", ".dross/gate/approval.json", ".dross/gate/writer.json")
	if g, err := LoadGreen(dir); err == nil || g != nil || !strings.Contains(err.Error(), "tracked") {
		t.Errorf("tracked green = %+v, %v; want a refusal", g, err)
	}
	if w, err := LoadWriter(dir); err == nil || w != nil || !strings.Contains(err.Error(), "tracked") {
		t.Errorf("tracked writer = %+v, %v; want a refusal", w, err)
	}
	if a, err := LoadApproval(dir); err == nil || a != nil || !strings.Contains(err.Error(), "tracked") {
		t.Errorf("tracked approval = %+v, %v; want a refusal", a, err)
	}
	if e, err := LoadExecute(dir); err != nil || e != nil {
		t.Errorf("untracked, absent execute = %+v, %v", e, err)
	}
}

func sampleReview() Review {
	return Review{Kind: review.KindTask, Phase: "p", Task: "t-3", Attempt: "base-abc", Rounds: []review.Round{
		{Outcome: review.OutcomeBlock, Spec: []review.Finding{{Criterion: "c-1", Severity: review.Blocking, Text: "S1"}},
			Quality: []review.Finding{{Severity: review.Flag, Text: "Q1"}}},
		{Outcome: review.OutcomePass, Tree: "tree-2", Digest: "sha256:d2"},
	}}
}

// TestReviewRecordDecode: missing is none, damaged is an error naming the
// file — never a zero record a gate could read as "no rounds yet".
func TestReviewRecordDecode(t *testing.T) {
	dir := t.TempDir()
	if r, err := LoadReview(dir); r != nil || err != nil {
		t.Errorf("missing review = %+v, %v; want nil, nil", r, err)
	}
	if q, err := LoadQuick(dir); q != nil || err != nil {
		t.Errorf("missing quick = %+v, %v; want nil, nil", q, err)
	}
	if err := SaveReview(dir, sampleReview()); err != nil {
		t.Fatal(err)
	}
	if err := SaveQuick(dir, Quick{Mode: "solo", Description: "fix x", Head: "h", At: at}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{ReviewFile, QuickFile} {
		if err := os.WriteFile(Path(dir, name), []byte(`{"kind":"task","rou`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if r, err := LoadReview(dir); err == nil || r != nil || !strings.Contains(err.Error(), ".dross/gate/review.json") {
		t.Errorf("a truncated review = %+v, %v; want an error naming .dross/gate/review.json", r, err)
	}
	if q, err := LoadQuick(dir); err == nil || q != nil || !strings.Contains(err.Error(), ".dross/gate/quick.json") {
		t.Errorf("a truncated quick = %+v, %v; want an error naming .dross/gate/quick.json", q, err)
	}
	if err := os.WriteFile(Path(dir, QuickFile), []byte(`{"mode":"turbo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if q, err := LoadQuick(dir); err == nil || q != nil {
		t.Errorf("a quick with an unknown mode = %+v, %v; want an error", q, err)
	}
	if err := RemoveQuick(dir); err != nil {
		t.Fatal(err)
	}
	if err := RemoveQuick(dir); err != nil {
		t.Errorf("removing an absent quick: %v", err)
	}
}

func TestReviewEmptyTreeRejected(t *testing.T) {
	dir := t.TempDir()
	r := sampleReview()
	r.Rounds[1].Tree = " "
	if err := SaveReview(dir, r); err == nil {
		t.Fatal("a pass round with no tree was saved")
	}
	if err := os.MkdirAll(filepath.Dir(Path(dir, ReviewFile)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir, ReviewFile), []byte(`{"kind":"task","attempt":"a","rounds":[{"outcome":"pass"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadReview(dir); err == nil || got != nil {
		t.Fatalf("a pass with no tree on disk = %+v, %v; want an error", got, err)
	}
	for _, bad := range []Review{{Kind: "x", Attempt: "a"}, {Kind: review.KindQuick}} {
		if err := SaveReview(dir, bad); err == nil {
			t.Errorf("SaveReview(%+v) succeeded", bad)
		}
	}
}

func TestReviewRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := sampleReview()
	if err := SaveReview(dir, want); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path(dir, ReviewFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"spec"`) || !strings.Contains(string(b), `"quality"`) {
		t.Fatalf("spec and quality findings are not kept apart on disk:\n%s", b)
	}
	got, err := LoadReview(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rounds) != 2 || got.Rounds[0].Spec[0].Text != "S1" || got.Rounds[0].Quality[0].Text != "Q1" ||
		got.Rounds[1].Tree != "tree-2" || got.Attempt != "base-abc" || got.Task != "t-3" {
		t.Fatalf("review round trip = %+v", got)
	}
}

// TestReviewConcurrentWrites: the ledger is written while hooks read it.
func TestReviewConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 1000)
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				r := sampleReview()
				r.Attempt = strings.Repeat(string(rune('a'+w)), 100+i)
				if err := SaveReview(dir, r); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	for k := 0; k < 4; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				r, err := LoadReview(dir)
				if err != nil {
					errs <- err
					continue
				}
				if r != nil && (len(r.Attempt) < 100 || len(r.Rounds) != 2) {
					errs <- os.ErrInvalid
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("a reader saw a partial review record: %v", err)
	}
}

func TestTrackedReviewRecordsRefused(t *testing.T) {
	dir := repo(t)
	if err := SaveReview(dir, sampleReview()); err != nil {
		t.Fatal(err)
	}
	if err := SaveQuick(dir, Quick{Mode: "solo", Description: "x", Head: "h", At: at}); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-f", ".dross/gate/review.json", ".dross/gate/quick.json")
	if r, err := LoadReview(dir); err == nil || r != nil || !strings.Contains(err.Error(), "tracked") {
		t.Errorf("tracked review = %+v, %v; want a refusal", r, err)
	}
	if q, err := LoadQuick(dir); err == nil || q != nil || !strings.Contains(err.Error(), "tracked") {
		t.Errorf("tracked quick = %+v, %v; want a refusal", q, err)
	}
}

func TestSaveContext(t *testing.T) {
	dir := repo(t)
	body := []byte("dross-review-context sha256:abc\n\nbody\n")
	if err := SaveContext(dir, body); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path(dir, ContextFile))
	if err != nil || string(b) != string(body) {
		t.Fatalf("context file = %q, %v", b, err)
	}
	if st := git(t, dir, "status", "--porcelain"); strings.Contains(st, "review-context") {
		t.Fatalf("the context file shows in git status:\n%s", st)
	}
}

func TestReviewPendingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := sampleReview()
	r.Pending = []Launch{{AgentID: "a1", Digest: "sha256:d", Tree: "t1"}}
	if err := SaveReview(dir, r); err != nil {
		t.Fatal(err)
	}
	got, err := LoadReview(dir)
	if err != nil || len(got.Pending) != 1 || got.Pending[0] != r.Pending[0] {
		t.Fatalf("pending launches did not round-trip: %+v, %v", got, err)
	}
	for _, bad := range []Launch{{Digest: "d", Tree: "t"}, {AgentID: "a", Tree: "t"}, {AgentID: "a", Digest: "d"}} {
		r.Pending = []Launch{bad}
		if err := SaveReview(dir, r); err == nil {
			t.Errorf("a pending launch %+v was accepted", bad)
		}
	}
}

// TestReplyApprovalCheck: an approval naming no PR or no digest is refused
// before anything is written, and a well-formed one saves.
func TestReplyApprovalCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    ReplyApproval
		want string // the refusal, or "" for a save that goes through
	}{
		{"PR 0", ReplyApproval{PR: 0, Digest: "d", At: at}, "names no PR"},
		{"PR -1", ReplyApproval{PR: -1, Digest: "d", At: at}, "names no PR"},
		{"empty digest", ReplyApproval{PR: 1, Digest: "", At: at}, "no digest"},
		{"blank digest", ReplyApproval{PR: 1, Digest: " \t", At: at}, "no digest"},
		{"PR 1", ReplyApproval{PR: 1, Digest: "d", At: at}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			err := SaveReplyApproval(dir, tc.a)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("SaveReplyApproval(%+v) = %v; want a refusal containing %q", tc.a, err, tc.want)
				}
				if _, statErr := os.Stat(Path(dir, ReplyFile)); !os.IsNotExist(statErr) {
					t.Errorf("a refused approval was written: stat = %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SaveReplyApproval(%+v) = %v", tc.a, err)
			}
			if got, err := LoadReplyApproval(dir); err != nil || got == nil || *got != tc.a {
				t.Errorf("load after save = %+v, %v; want %+v", got, err, tc.a)
			}
		})
	}
}

// TestLoadReplyApprovalRefusesBadFile: a missing reply.json reads as no
// approval, a valid one round-trips, and one that is malformed or fails check()
// is an error, never a usable approval.
func TestLoadReplyApprovalRefusesBadFile(t *testing.T) {
	dir := t.TempDir()
	if a, err := LoadReplyApproval(dir); a != nil || err != nil {
		t.Errorf("missing reply approval = %+v, %v; want nil, nil", a, err)
	}
	want := ReplyApproval{PR: 7, Digest: "sha256:abc", At: at}
	if err := SaveReplyApproval(dir, want); err != nil {
		t.Fatal(err)
	}
	if a, err := LoadReplyApproval(dir); err != nil || a == nil || *a != want {
		t.Errorf("reply approval round trip = %+v, %v; want %+v", a, err, want)
	}
	for name, body := range map[string]string{
		"malformed": `{"pr":7,"digest":"sha`,
		"PR 0":      `{"pr":0,"digest":"sha256:abc","at":"2026-10-03T09:00:00Z"}`,
		"no digest": `{"pr":7,"digest":"","at":"2026-10-03T09:00:00Z"}`,
	} {
		if err := os.WriteFile(Path(dir, ReplyFile), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if a, err := LoadReplyApproval(dir); err == nil || a != nil || !strings.Contains(err.Error(), ".dross/gate/reply.json") {
			t.Errorf("%s reply.json = %+v, %v; want an error naming .dross/gate/reply.json", name, a, err)
		}
	}
}

func sampleWriter() Writer {
	return Writer{Phase: "p", Task: "t-3", AgentID: "abd48e82da367fc86", State: WriterLaunched}
}

// TestWriterRecordDecode: missing is none, damaged is an error naming the
// file — never a zero record a gate could read as "no writer yet".
func TestWriterRecordDecode(t *testing.T) {
	dir := t.TempDir()
	if w, err := LoadWriter(dir); w != nil || err != nil {
		t.Errorf("missing writer = %+v, %v; want nil, nil", w, err)
	}
	stopped := sampleWriter()
	stopped.State, stopped.StoppedAt = WriterStopped, &at
	if err := SaveWriter(dir, stopped); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWriter(dir)
	if err != nil || got == nil || got.AgentID != stopped.AgentID || got.State != WriterStopped || got.StoppedAt == nil || !got.StoppedAt.Equal(at) {
		t.Fatalf("round trip = %+v, %v; want %+v", got, err, stopped)
	}
	if err := os.WriteFile(Path(dir, WriterFile), []byte(`{"phase":"p","ta`), 0o644); err != nil {
		t.Fatal(err)
	}
	if w, err := LoadWriter(dir); err == nil || w != nil || !strings.Contains(err.Error(), ".dross/gate/writer.json") {
		t.Errorf("a truncated writer = %+v, %v; want an error naming .dross/gate/writer.json", w, err)
	}
	for _, body := range []string{`{}`, `{"phase":"p","task":"t-3","agent_id":"a","state":"running"}`, `{"phase":"p","task":"t-3","state":"launched"}`} {
		if err := os.WriteFile(Path(dir, WriterFile), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if w, err := LoadWriter(dir); err == nil || w != nil {
			t.Errorf("writer %s = %+v, %v; want an error", body, w, err)
		}
	}
	bad := sampleWriter()
	bad.AgentID = ""
	if err := SaveWriter(dir, bad); err == nil {
		t.Error("a writer with no agent id was saved")
	}
	if err := ClearWriter(dir); err != nil {
		t.Fatal(err)
	}
	if err := ClearWriter(dir); err != nil {
		t.Errorf("clearing an absent writer: %v", err)
	}
	if w, err := LoadWriter(dir); w != nil || err != nil {
		t.Errorf("cleared writer = %+v, %v; want nil, nil", w, err)
	}
}

// laneFingerprints are the real fingerprints of two lanes: a prepared lane,
// whose LaneLine carries NULs, and a bare one.
func laneFingerprints() []string {
	return []string{
		consent.Fingerprint(consent.LaneLine(project.TestLane{Name: "web", Prepare: "npm ci", Command: "npx vitest related --run"})),
		consent.Fingerprint(consent.LaneLine(project.TestLane{Name: "go", Command: "go test"})),
	}
}

func sampleScoped() Scoped {
	return Scoped{
		Tree:    "4b825dc",
		Covered: []string{"src/a.ts", "src/b.svelte"},
		Lanes:   laneFingerprints(),
		At:      at,
		Runner:  "helicon",
	}
}

// TestScopedRoundTrip: every field survives the file, and a later save replaces
// the record rather than unioning with it.
func TestScopedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := sampleScoped()
	if err := SaveScoped(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadScoped(dir)
	if err != nil || got == nil {
		t.Fatalf("LoadScoped = %+v, %v", got, err)
	}
	if got.Tree != want.Tree || !got.At.Equal(want.At) || got.Runner != want.Runner {
		t.Errorf("scalar fields = %+v; want %+v", got, want)
	}
	if !slices.Equal(got.Covered, want.Covered) {
		t.Errorf("Covered = %q; want %q", got.Covered, want.Covered)
	}
	if !slices.Equal(got.Lanes, laneFingerprints()) {
		t.Errorf("Lanes = %q; want the LaneLine fingerprints %q", got.Lanes, laneFingerprints())
	}

	next := sampleScoped()
	next.Covered = []string{"src/c.ts"}
	if err := SaveScoped(dir, next); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadScoped(dir); err != nil || !slices.Equal(got.Covered, next.Covered) {
		t.Errorf("after a second save = %+v, %v; want Covered %q alone", got, err, next.Covered)
	}
}

// TestSaveScopedRefusesHollowRecord: a record with nothing to vouch for is
// refused before anything is written.
func TestSaveScopedRefusesHollowRecord(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spoil func(*Scoped)
		want  string
	}{
		{"empty tree", func(s *Scoped) { s.Tree = "" }, "no tree"},
		{"blank tree", func(s *Scoped) { s.Tree = " \t" }, "no tree"},
		{"nil covered", func(s *Scoped) { s.Covered = nil }, "covers no path"},
		{"empty covered", func(s *Scoped) { s.Covered = []string{} }, "covers no path"},
		{"blank covered path", func(s *Scoped) { s.Covered = []string{"src/a.ts", " "} }, "covered path 2 is blank"},
		{"nil lanes", func(s *Scoped) { s.Lanes = nil }, "names no lane"},
		{"blank lane", func(s *Scoped) { s.Lanes = []string{"abc", ""} }, "lane fingerprint 2 is blank"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s := sampleScoped()
			tc.spoil(&s)
			err := SaveScoped(dir, s)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), ".dross/gate/scoped.json") {
				t.Fatalf("SaveScoped(%+v) = %v; want a refusal naming .dross/gate/scoped.json and containing %q", s, err, tc.want)
			}
			if _, statErr := os.Stat(Path(dir, ScopedFile)); !os.IsNotExist(statErr) {
				t.Errorf("a refused record was written: stat = %v", statErr)
			}
		})
	}
}

// TestLoadScopedRefusesBadFile: no file is no record; a file that does not
// decode, or decodes hollow, is an error naming the path, never a zero value a
// gate could read as a scoped green.
func TestLoadScopedRefusesBadFile(t *testing.T) {
	dir := t.TempDir()
	if s, err := LoadScoped(dir); s != nil || err != nil {
		t.Errorf("missing scoped = %+v, %v; want nil, nil", s, err)
	}
	if err := SaveScoped(dir, sampleScoped()); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"truncated":   `{"tree":"abc","cov`,
		"empty file":  ``,
		"wrong type":  `{"tree":"abc","covered":"src/a.ts","lanes":["f"]}`,
		"zero record": `{}`,
		"no tree":     `{"tree":"","covered":["src/a.ts"],"lanes":["f"]}`,
		"no covered":  `{"tree":"abc","covered":[],"lanes":["f"]}`,
		"no lanes":    `{"tree":"abc","covered":["src/a.ts"]}`,
	} {
		if err := os.WriteFile(Path(dir, ScopedFile), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if s, err := LoadScoped(dir); err == nil || s != nil || !strings.Contains(err.Error(), ".dross/gate/scoped.json") {
			t.Errorf("%s scoped.json = %+v, %v; want an error naming .dross/gate/scoped.json", name, s, err)
		}
	}
}

// TestTrackedScopedRefusedUnread: the file holds bytes that would not decode,
// so only a guard running before the read returns the tracked refusal.
func TestTrackedScopedRefusedUnread(t *testing.T) {
	dir := repo(t)
	if err := SaveScoped(dir, sampleScoped()); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-f", ".dross/gate/scoped.json")
	if s, err := LoadScoped(dir); err == nil || s != nil || !strings.Contains(err.Error(), "tracked") {
		t.Errorf("tracked scoped = %+v, %v; want a refusal", s, err)
	}
	if err := os.WriteFile(Path(dir, ScopedFile), []byte(`{"tree":`), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadScoped(dir); err == nil || s != nil || !strings.Contains(err.Error(), "tracked") {
		t.Errorf("tracked, undecodable scoped = %+v, %v; want the tracked refusal, not a decode error", s, err)
	}
}

// TestScopedAndGreenStaySeparate: neither record stands in for the other, and
// writing one never touches the other's file.
func TestScopedAndGreenStaySeparate(t *testing.T) {
	dir := t.TempDir()
	if err := SaveScoped(dir, sampleScoped()); err != nil {
		t.Fatal(err)
	}
	if g, err := LoadGreen(dir); g != nil || err != nil {
		t.Errorf("green with only scoped.json present = %+v, %v; want nil, nil", g, err)
	}

	dir = t.TempDir()
	if err := SaveGreen(dir, Green{Tree: "4b825dc", At: at, Runner: "local"}); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadScoped(dir); s != nil || err != nil {
		t.Errorf("scoped with only green.json present = %+v, %v; want nil, nil", s, err)
	}
	before, err := os.ReadFile(Path(dir, GreenFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveScoped(dir, sampleScoped()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(Path(dir, GreenFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("SaveScoped changed green.json:\nbefore %s\nafter  %s", before, after)
	}
}

// TestClearScoped: clearing removes the scoped record alone, and clearing
// nothing is not an error.
func TestClearScoped(t *testing.T) {
	dir := t.TempDir()
	if err := ClearScoped(dir); err != nil {
		t.Errorf("clearing with no gate dir: %v", err)
	}
	if err := SaveGreen(dir, Green{Tree: "4b825dc", At: at, Runner: "local"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(Path(dir, GreenFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := ClearScoped(dir); err != nil {
		t.Errorf("clearing an absent scoped record beside a green: %v", err)
	}
	if err := SaveScoped(dir, sampleScoped()); err != nil {
		t.Fatal(err)
	}
	if err := ClearScoped(dir); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadScoped(dir); s != nil || err != nil {
		t.Errorf("after ClearScoped = %+v, %v; want nil, nil", s, err)
	}
	after, err := os.ReadFile(Path(dir, GreenFile))
	if err != nil {
		t.Fatalf("ClearScoped took green.json with it: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("ClearScoped changed green.json:\nbefore %s\nafter  %s", before, after)
	}
}
