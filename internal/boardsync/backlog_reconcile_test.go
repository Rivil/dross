package boardsync

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/deferred"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/milestone"
)

// The inbound half of backlog sync: SyncBacklog end to end over a TempDir
// .dross root, and the reconcile pass that closes mirrors only when their
// artefact can be shown resolved.

// writeMilestonePhases saves milestone version under root with the given
// roadmap phases.
func writeMilestonePhases(t *testing.T, root, version string, phases ...string) {
	t.Helper()
	m := &milestone.Milestone{}
	m.Milestone.Version = version
	m.Milestone.Status = "active"
	m.Phases = phases
	if err := m.Save(milestone.FilePath(root, version)); err != nil {
		t.Fatal(err)
	}
}

// mkPhaseDir creates an (empty) phase directory — the scaffolding proof.
func mkPhaseDir(t *testing.T, root, slug string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "phases", slug), 0o755); err != nil {
		t.Fatal(err)
	}
}

const shippedSpec = `[phase]
id = "shipped"
title = "Shipped"

[[deferred]]
id = "d1"
text = "plain"

[[deferred]]
id = "d2"
text = "routed"
target = "todo"

[[deferred]]
id = "d3"
text = "dismissed"
dismissed = true
`

// TestSyncBacklogMirrorsUnscaffoldedSlugsAndLiveDeferred: an unscaffolded
// roadmap slug and every non-dismissed deferred idea get a mirror; a
// scaffolded slug and a dismissed idea do not. A second run updates all three.
func TestSyncBacklogMirrorsUnscaffoldedSlugsAndLiveDeferred(t *testing.T) {
	f := newFaultBoard()
	ctx, out := boardCtx(t, f)
	writeMilestonePhases(t, ctx.Root, "v1", "shipped", "todo")
	writeSpec(t, filepath.Dir(ctx.Root), "shipped", shippedSpec)

	var err error
	captureStderr(t, func() { err = SyncBacklog(ctx, "v1") })
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, in := range f.created {
		titles = append(titles, in.Title)
	}
	sort.Strings(titles)
	if want := []string{"[backlog] todo", "[routed] routed", "[someday] plain"}; !reflect.DeepEqual(titles, want) {
		t.Errorf("created %v, want %v", titles, want)
	}
	if got := out.String(); got != "backlog v1 -> 3 created, 0 updated, 0 closed\n" {
		t.Errorf("narration = %q", got)
	}
	saved, err := board.Load(ctx.BoardPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := saved.BacklogKeys(), []string{"slug:todo", "someday:id:d1", "someday:id:d2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("board.json backlog keys = %v, want %v", got, want)
	}

	out.Reset()
	captureStderr(t, func() { err = SyncBacklog(ctx, "v1") })
	if err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "backlog v1 -> 0 created, 3 updated, 0 closed\n" {
		t.Errorf("second narration = %q", got)
	}
	if len(f.created) != 3 {
		t.Errorf("the second run created again (%d total)", len(f.created))
	}
}

// TestSyncBacklogMissingMilestoneNamesIt: an unknown version is an error that
// names it, before the board is touched at all.
func TestSyncBacklogMissingMilestoneNamesIt(t *testing.T) {
	f := newFaultBoard()
	ctx, out := boardCtx(t, f)
	err := SyncBacklog(ctx, "v9")
	if err == nil || !strings.Contains(err.Error(), `load milestone "v9"`) {
		t.Errorf("err = %v, want it to name v9", err)
	}
	if len(f.calls) != 0 || out.Len() != 0 {
		t.Errorf("board calls %v, narration %q; want none", f.calls, out.String())
	}
}

// TestSyncBacklogSurfacesStepErrors: a deferred store that cannot be read, or
// a push that fails, returns the error and narrates nothing.
func TestSyncBacklogSurfacesStepErrors(t *testing.T) {
	t.Run("unreadable spec", func(t *testing.T) {
		f := newFaultBoard()
		ctx, out := boardCtx(t, f)
		writeMilestonePhases(t, ctx.Root, "v1")
		writeSpec(t, filepath.Dir(ctx.Root), "broken", "[phase\nnot toml")
		if err := SyncBacklog(ctx, "v1"); err == nil {
			t.Error("a malformed spec.toml was swallowed")
		}
		if len(f.calls) != 0 || out.Len() != 0 {
			t.Errorf("board calls %v, narration %q; want none", f.calls, out.String())
		}
	})
	t.Run("push fails", func(t *testing.T) {
		f := newFaultBoard()
		f.milestoneErr = errors.New("no milestones")
		ctx, out := boardCtx(t, f)
		writeMilestonePhases(t, ctx.Root, "v1", "todo")
		if err := SyncBacklog(ctx, "v1"); err == nil {
			t.Error("a failed push was swallowed")
		}
		if out.Len() != 0 {
			t.Errorf("narration %q after a failed push", out.String())
		}
	})
}

// reconcileFixture lays out one mirror per reconcile branch. See
// TestReconcileBacklogClosesOnlyProvablyResolvedMirrors for what each is.
func reconcileFixture(t *testing.T) (*Ctx, *faultBoard, []BacklogItem, []deferred.Entry) {
	t.Helper()
	f := newFaultBoard()
	for _, iss := range []forge.Issue{
		{Key: "PROJ-1"}, {Key: "PROJ-2"}, {Key: "PROJ-3"}, {Key: "PROJ-4"},
		{Key: "PROJ-5"}, {Key: "PROJ-6"}, {Key: "PROJ-7", State: "closed"},
		{Key: "PROJ-8", State: "closed"}, {Key: "PROJ-9"},
	} {
		f.seed(iss)
	}
	ctx, _ := boardCtx(t, f)
	for _, slug := range []string{"built", "built2", "empty"} {
		mkPhaseDir(t, ctx.Root, slug)
	}
	ctx.Board.SetPhase("shipped", "PROJ-8")
	ctx.Board.SetPhase("open-phase", "PROJ-9")
	for key, issue := range map[string]string{
		"slug:built":    "PROJ-1", // scaffolded: resolved, closed, key dropped
		"slug:ghost":    "PROJ-2", // left the set with no dir: unattributable
		"someday:id:dz": "PROJ-3", // dismissed, by id key
		"someday:ph#5":  "PROJ-4", // dismissed, by legacy positional key
		"someday:id:r1": "PROJ-5", // live, routed to a done target: closed, key KEPT
		"someday:id:r2": "PROJ-6", // live, routed to an open target: untouched
		"slug:built2":   "PROJ-7", // resolved but already closed: no second close
		"slug:empty":    "",       // no issue id: skipped
	} {
		ctx.Board.SetBacklog(key, issue)
	}
	live := []BacklogItem{
		{Key: "someday:id:r1", Target: "shipped"},
		{Key: "someday:id:r2", Target: "open-phase"},
	}
	items := []deferred.Entry{
		{ID: "dz", Source: "ph", Index: 4, Dismissed: true},
		{ID: "dy", Source: "ph", Index: 5, Dismissed: true},
	}
	return ctx, f, live, items
}

// TestReconcileBacklogClosesOnlyProvablyResolvedMirrors: set-difference alone
// never closes a mirror — each close is backed by a phase directory, a
// dismissal, or a done routing target.
func TestReconcileBacklogClosesOnlyProvablyResolvedMirrors(t *testing.T) {
	ctx, f, live, items := reconcileFixture(t)
	var n int
	var err error
	stderr := captureStderr(t, func() { n, err = ReconcileBacklog(ctx, live, items) })
	if err != nil || n != 4 {
		t.Fatalf("ReconcileBacklog = (%d, %v), want (4, nil)", n, err)
	}
	closed := append([]string(nil), f.closed...)
	sort.Strings(closed)
	if want := []string{"PROJ-1", "PROJ-3", "PROJ-4", "PROJ-5"}; !reflect.DeepEqual(closed, want) {
		t.Errorf("closed %v, want %v", closed, want)
	}
	if got, want := ctx.Board.BacklogKeys(), []string{"slug:built2", "slug:empty", "slug:ghost", "someday:id:r1", "someday:id:r2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("remaining keys = %v, want %v (a live routed mirror keeps its link)", got, want)
	}
	if !strings.Contains(stderr, "PROJ-2 (slug:ghost)") || !strings.Contains(stderr, "leaving it open") {
		t.Errorf("stderr does not name the unattributable mirror:\n%s", stderr)
	}
	for _, c := range f.calls {
		if c == "CloseIssue PROJ-7" {
			t.Error("an already-resolved mirror was closed again")
		}
		if c == "GetIssue " || c == "CloseIssue " {
			t.Errorf("an empty issue id reached the board: %q", c)
		}
	}
	if f.issues["PROJ-6"].State != "open" || f.issues["PROJ-2"].State != "open" {
		t.Error("a mirror with no proof of resolution was closed")
	}
}

// TestReconcileBacklogKeepsLinkWhenCloseFails: a failed close or read-back
// warns and keeps the link, so the next run retries rather than stranding
// the issue.
func TestReconcileBacklogKeepsLinkWhenCloseFails(t *testing.T) {
	ctx, f, live, items := reconcileFixture(t)
	f.failClose["PROJ-1"] = errors.New("refused")
	var n int
	stderr := captureStderr(t, func() { n, _ = ReconcileBacklog(ctx, live, items) })
	if n != 3 {
		t.Errorf("closed %d, want 3 (the failed close is not counted)", n)
	}
	if id, ok := ctx.Board.BacklogID("slug:built"); !ok || id != "PROJ-1" {
		t.Error("a failed close dropped the link")
	}
	if !strings.Contains(stderr, "could not close backlog mirror PROJ-1") || !strings.Contains(stderr, "the link is kept") {
		t.Errorf("stderr = %q", stderr)
	}

	ctx, f, live, items = reconcileFixture(t)
	f.failGet["PROJ-1"] = errors.New("unreachable")
	stderr = captureStderr(t, func() { n, _ = ReconcileBacklog(ctx, live, items) })
	if n != 3 || !strings.Contains(stderr, "could not read PROJ-1 back") {
		t.Errorf("closed %d, stderr %q; want 3 and a read-back warning", n, stderr)
	}
	for _, c := range f.calls {
		if c == "CloseIssue PROJ-1" {
			t.Error("a mirror that could not be read was closed")
		}
	}
	if _, ok := ctx.Board.BacklogID("slug:built"); !ok {
		t.Error("an unreadable mirror lost its link")
	}
}

// TestBacklogVerdictTable: one row per BacklogVerdictFor branch, and the
// IssueIsDone split it leans on.
func TestBacklogVerdictTable(t *testing.T) {
	f := newFaultBoard()
	f.seed(forge.Issue{Key: "PROJ-20", State: "closed"})
	f.seed(forge.Issue{Key: "PROJ-21"})
	f.seed(forge.Issue{Key: "PROJ-22"})
	f.failGet["PROJ-22"] = errors.New("boom")
	ctx, _ := boardCtx(t, f)
	ctx.Board.SetPhase("done-phase", "PROJ-20")
	ctx.Board.SetPhase("open-phase", "PROJ-21")
	ctx.Board.SetPhase("err-phase", "PROJ-22")
	mkPhaseDir(t, ctx.Root, "has-dir")

	live := map[string]BacklogItem{
		"k-unrouted": {Key: "k-unrouted"},
		"k-notarget": {Key: "k-notarget", Target: "no-issue-phase"},
		"k-done":     {Key: "k-done", Target: "done-phase"},
		"k-open":     {Key: "k-open", Target: "open-phase"},
		"k-err":      {Key: "k-err", Target: "err-phase"},
	}
	def := map[string]deferred.Entry{
		"someday:id:dd": {ID: "dd", Dismissed: true},
		"someday:id:dl": {ID: "dl"},
		"someday:ph#0":  {Source: "ph", Index: 0},
	}
	for _, tc := range []struct {
		key  string
		want BacklogVerdict
	}{
		{"k-unrouted", BacklogStillOpen},
		{"k-notarget", BacklogStillOpen},
		{"k-done", BacklogResolved},
		{"k-open", BacklogStillOpen},
		{"k-err", BacklogStillOpen},
		{"slug:has-dir", BacklogResolved},
		{"slug:no-dir", BacklogUnattributable},
		{"someday:id:dd", BacklogResolved},
		{"someday:id:dl", BacklogStillOpen},
		{"someday:ph#0", BacklogStillOpen},
		{"mystery", BacklogUnattributable},
	} {
		if got := BacklogVerdictFor(ctx, tc.key, live, def); got != tc.want {
			t.Errorf("BacklogVerdictFor(%q) = %d, want %d", tc.key, got, tc.want)
		}
	}

	g := newFaultBoard()
	g.seed(forge.Issue{Key: "PROJ-30", Resolved: true})
	g.seed(forge.Issue{Key: "PROJ-31", State: "closed"})
	g.seed(forge.Issue{Key: "PROJ-32"})
	g.nilGet["PROJ-33"] = true
	g.failGet["PROJ-34"] = errors.New("boom")
	gctx, _ := boardCtx(t, g)
	for _, tc := range []struct {
		key  string
		done bool
	}{{"PROJ-30", true}, {"PROJ-31", true}, {"PROJ-32", false}, {"PROJ-33", false}} {
		if done, err := IssueIsDone(gctx, tc.key); err != nil || done != tc.done {
			t.Errorf("IssueIsDone(%s) = (%v, %v), want (%v, nil)", tc.key, done, err, tc.done)
		}
	}
	if done, err := IssueIsDone(gctx, "PROJ-34"); done || err == nil || !strings.HasPrefix(err.Error(), "board:") {
		t.Errorf("IssueIsDone on a read error = (%v, %v), want (false, board: error)", done, err)
	}
}
