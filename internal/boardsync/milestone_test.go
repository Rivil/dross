package boardsync

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// writeMilestoneScope saves milestone version with a title and success
// criteria.
func writeMilestoneScope(t *testing.T, root, version, title string, criteria ...string) {
	t.Helper()
	m := &milestone.Milestone{}
	m.Milestone.Version = version
	m.Milestone.Title = title
	m.Scope.SuccessCriteria = criteria
	if err := m.Save(milestone.FilePath(root, version)); err != nil {
		t.Fatal(err)
	}
}

// TestEnsureMilestoneLinkRecordsBoardID: the link is read from the cache when
// present, skipped when the milestone is not on disk, and otherwise ensured on
// the board and recorded — for the default backend, YouTrack's epic mode and
// Jira's version path alike.
func TestEnsureMilestoneLinkRecordsBoardID(t *testing.T) {
	t.Run("cached", func(t *testing.T) {
		f := newFaultBoard()
		ctx, _ := boardCtx(t, f)
		ctx.Board.SetMilestone("v1", "42")
		if id, err := EnsureMilestoneLink(ctx, "v1"); id != "42" || err != nil || len(f.milestones) != 0 {
			t.Errorf("= (%q, %v) with %d board calls, want (42, nil) and none", id, err, len(f.milestones))
		}
	})

	t.Run("no milestone on disk", func(t *testing.T) {
		f := newFaultBoard()
		ctx, _ := boardCtx(t, f)
		if id, err := EnsureMilestoneLink(ctx, "v1"); id != "" || err != nil || len(f.milestones) != 0 {
			t.Errorf("= (%q, %v) with %d board calls, want (\"\", nil) and none", id, err, len(f.milestones))
		}
	})

	t.Run("titled", func(t *testing.T) {
		f := newFaultBoard()
		ctx, _ := boardCtx(t, f)
		writeMilestoneScope(t, ctx.Root, "v1", "Launch", "c1", "c2")
		id, err := EnsureMilestoneLink(ctx, "v1")
		if id != "7" || err != nil {
			t.Fatalf("= (%q, %v), want (7, nil)", id, err)
		}
		want := []milestoneCall{{Title: "v1", Body: "Launch\n\nSuccess criteria:\nc1\nc2"}}
		if !reflect.DeepEqual(f.milestones, want) {
			t.Errorf("EnsureMilestone calls = %q, want %q", f.milestones, want)
		}
		if got, _ := ctx.Board.MilestoneID("v1"); got != "7" {
			t.Errorf("recorded %q, want 7", got)
		}
	})

	t.Run("untitled", func(t *testing.T) {
		f := newFaultBoard()
		ctx, _ := boardCtx(t, f)
		writeMilestoneScope(t, ctx.Root, "v1", "", "c1")
		if _, err := EnsureMilestoneLink(ctx, "v1"); err != nil {
			t.Fatal(err)
		}
		if len(f.milestones) != 1 || f.milestones[0].Body != "v1\n\nSuccess criteria:\nc1" {
			t.Errorf("EnsureMilestone calls = %q, want the version as the title", f.milestones)
		}
	})

	t.Run("board error", func(t *testing.T) {
		f := newFaultBoard()
		f.milestoneErr = errors.New("refused")
		ctx, _ := boardCtx(t, f)
		writeMilestoneScope(t, ctx.Root, "v1", "Launch")
		id, err := EnsureMilestoneLink(ctx, "v1")
		if id != "" || err == nil || !strings.HasPrefix(err.Error(), "board:") {
			t.Errorf("= (%q, %v), want (\"\", board: error)", id, err)
		}
		if _, ok := ctx.Board.MilestoneID("v1"); ok {
			t.Error("a failed ensure recorded a link")
		}
	})

	t.Run("backend ensured nothing", func(t *testing.T) {
		f := newFaultBoard()
		f.milestoneID = ""
		ctx, _ := boardCtx(t, f)
		writeMilestoneScope(t, ctx.Root, "v1", "Launch")
		if id, err := EnsureMilestoneLink(ctx, "v1"); id != "" || err != nil {
			t.Errorf("= (%q, %v), want (\"\", nil)", id, err)
		}
		if _, ok := ctx.Board.MilestoneID("v1"); ok {
			t.Error("an empty id was recorded")
		}
	})

	t.Run("youtrack epic", func(t *testing.T) {
		tr := strictTracker(t)
		tr.on("GET", "/api/issues", `[]`)
		tr.on("POST", "/api/issues", `{"idReadable":"PROJ-77","summary":"v1"}`)
		_, ctx := ytRepoMode(t, tr, `{}`, "epic")
		writeMilestoneScope(t, ctx.Root, "v1", "Launch", "c1")
		if id, err := EnsureMilestoneLink(ctx, "v1"); id != "PROJ-77" || err != nil {
			t.Fatalf("= (%q, %v), want (PROJ-77, nil)", id, err)
		}
		if got, _ := ctx.Board.MilestoneID("v1"); got != "PROJ-77" {
			t.Errorf("recorded %q, want the epic's idReadable", got)
		}
	})

	t.Run("jira version", func(t *testing.T) {
		tr := strictTracker(t)
		tr.on("GET", "/rest/api/3/project/PROJ", `{"id":"10000","versions":[]}`)
		tr.on("POST", "/rest/api/3/version", `{"id":"555"}`)
		_, ctx := jiraRepo(t, tr, `{}`)
		writeMilestoneScope(t, ctx.Root, "v1", "Launch", "c1")
		if id, err := EnsureMilestoneLink(ctx, "v1"); id != "555" || err != nil {
			t.Fatalf("= (%q, %v), want (555, nil)", id, err)
		}
		if posts := tr.callsTo("POST", "/rest/api/3/version"); len(posts) != 1 || !strings.Contains(posts[0].Body, `"name":"v1"`) {
			t.Errorf("version create = %v", posts)
		}
	})
}

// TestCheckMilestoneClosableOnlyForYouTrackEpic: --close is refused unless the
// milestone is itself an issue — YouTrack's epic mode, however the mode is
// spelled.
func TestCheckMilestoneClosableOnlyForYouTrackEpic(t *testing.T) {
	ctx, _ := boardCtx(t, newFaultBoard())
	if err := CheckMilestoneClosable(ctx); err == nil || !strings.Contains(err.Error(), "fakeboard") {
		t.Errorf("non-YouTrack board = %v, want a refusal naming the provider", err)
	}

	for _, tc := range []struct {
		mode, want string
	}{
		{"", `"version"`},
		{"agile", `"agile"`},
		{"version", `"version"`},
	} {
		_, yctx := ytRepoMode(t, strictTracker(t), `{}`, tc.mode)
		if err := CheckMilestoneClosable(yctx); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("mode %q = %v, want a refusal quoting %s", tc.mode, err, tc.want)
		}
	}

	_, yctx := ytRepoMode(t, strictTracker(t), `{}`, " Epic ")
	if err := CheckMilestoneClosable(yctx); err != nil {
		t.Errorf("mode \" Epic \" = %v, want nil", err)
	}

	if got := MilestoneBody("t", ""); got != "t" {
		t.Errorf("MilestoneBody without criteria = %q, want the bare title", got)
	}
}
