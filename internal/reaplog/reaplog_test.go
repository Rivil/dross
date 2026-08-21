package reaplog_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/reaplog"
)

func sampleRun() reaplog.Run {
	return reaplog.Run{
		StartedAt: time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC),
		Cards: []reaplog.Card{
			{
				Issue: "DRO-33", Class: "Phases",
				PriorState: "In Review", PriorResolved: false,
				PriorLabels: []string{"dross", "dross/status:in-progress"},
				Outcome:     reaplog.OutcomeClosed,
			},
			{
				Issue: "DRO-95", Class: "Backlog",
				PriorState: "Submitted", DroppedLink: "slug:future-x",
				Outcome: reaplog.OutcomeClosed,
			},
		},
	}
}

// TestJournalRoundTripsPriorState: prior_state is the whole reason the ledger
// exists — a card restored to a blanket "Open" is not restored. An entry that
// loses it on the way to disk must fail here, not silently produce a lossy undo
// months later.
func TestJournalRoundTripsPriorState(t *testing.T) {
	path := filepath.Join(t.TempDir(), reaplog.File)
	want := sampleRun()

	l := &reaplog.Log{}
	l.Append(want)
	if err := l.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := reaplog.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(got.Runs))
	}
	if !got.Runs[0].StartedAt.Equal(want.StartedAt) {
		t.Errorf("started_at = %v, want %v", got.Runs[0].StartedAt, want.StartedAt)
	}
	if !reflect.DeepEqual(got.Runs[0].Cards, want.Cards) {
		t.Errorf("cards round-tripped lossily:\n got %+v\nwant %+v", got.Runs[0].Cards, want.Cards)
	}
	// Named explicitly as well as by DeepEqual: a struct tag typo on this one
	// field turns undo into a no-op, and a DeepEqual failure alone would not
	// say which field went missing.
	if got.Runs[0].Cards[0].PriorState != "In Review" {
		t.Errorf("prior_state = %q, want %q", got.Runs[0].Cards[0].PriorState, "In Review")
	}
	if got.Runs[0].Cards[1].DroppedLink != "slug:future-x" {
		t.Errorf("dropped_link = %q, want the backlog key", got.Runs[0].Cards[1].DroppedLink)
	}
}

// TestMissingJournalIsNotAnError: a first --undo on a repo that has never
// applied must be able to say "nothing to undo" rather than fail.
func TestMissingJournalIsNotAnError(t *testing.T) {
	l, err := reaplog.Load(filepath.Join(t.TempDir(), reaplog.File))
	if err != nil {
		t.Fatalf("load of an absent ledger errored: %v", err)
	}
	if l == nil {
		t.Fatal("load returned a nil log with a nil error")
	}
	if len(l.Runs) != 0 {
		t.Errorf("runs = %d, want an empty log", len(l.Runs))
	}
	if l.Last() != nil {
		t.Error("Last on an empty log must be nil — there is no undo target")
	}
}

// TestFailedCardIsNotUndoable: a card whose close failed was never moved.
// Replaying a restore over it would write a state the sweep is not responsible
// for.
func TestFailedCardIsNotUndoable(t *testing.T) {
	r := reaplog.Run{Cards: []reaplog.Card{
		{Issue: "DRO-1", Outcome: reaplog.OutcomeClosed},
		{Issue: "DRO-2", Outcome: reaplog.OutcomeFailed},
		{Issue: "DRO-3", Outcome: reaplog.OutcomeClosed},
	}}
	l := &reaplog.Log{}
	l.Append(r)

	var got []string
	for _, c := range l.Last().Closed() {
		got = append(got, c.Issue)
	}
	want := []string{"DRO-1", "DRO-3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Closed() = %v, want %v — a failed card must not be undoable", got, want)
	}
}

// TestLastIsTheMostRecentRun: undo reverses the last run only.
func TestLastIsTheMostRecentRun(t *testing.T) {
	l := &reaplog.Log{}
	l.Append(reaplog.Run{Cards: []reaplog.Card{{Issue: "old", Outcome: reaplog.OutcomeClosed}}})
	l.Append(reaplog.Run{Cards: []reaplog.Card{{Issue: "new", Outcome: reaplog.OutcomeClosed}}})

	closed := l.Last().Closed()
	if len(closed) != 1 || closed[0].Issue != "new" {
		t.Errorf("Last().Closed() = %+v, want only the second run's card", closed)
	}
}

// TestReapLogIsNotABoardNamespace: the ledger lives in its own package on
// purpose. A map on board.Board would register as a mirror namespace with the
// guard that demands every namespace have a reap path — and the ledger is not a
// mirror. This pins the namespace set so adding one there is a deliberate act.
func TestReapLogIsNotABoardNamespace(t *testing.T) {
	known := map[string]bool{"Milestones": true, "Phases": true, "Quicks": true, "Backlog": true, "Tasks": true}
	rt := reflect.TypeOf(board.Board{})
	found := 0
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Type.Kind() != reflect.Map {
			continue
		}
		found++
		if !known[f.Name] {
			t.Errorf("board.Board gained the map field %q — if it is a mirror namespace it needs a reap lane, and if it is a ledger it does not belong on Board", f.Name)
		}
	}
	if found != len(known) {
		t.Errorf("found %d map fields on board.Board, want %d — the pinned set is stale", found, len(known))
	}
}

// TestReapLogIsGitignored: a tracked ledger dirties the tree on every apply and
// ship's clean-tree auto-commit then carries it into the squash, dragging a
// stale undo target into every later tree.
func TestReapLogIsGitignored(t *testing.T) {
	root := repoRoot(t)
	rel := filepath.Join(".dross", reaplog.File)
	cmd := exec.Command("git", "-C", root, "check-ignore", rel)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git check-ignore %s: %v (%s) — the ledger must be gitignored", rel, err, strings.TrimSpace(string(out)))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}
