package boardsync

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/deferred"
	"github.com/Rivil/dross/internal/forge"
)

// The push half of backlog sync: PushBacklogItems and the resolvers it leans
// on, over the in-memory doubles and — for the YouTrack-only create path —
// the strict scripted tracker.

func plainEntry() deferred.Entry {
	return deferred.Entry{ID: "p1", Source: "ph", Index: 0, Text: "plain idea"}
}

func routedEntry(target string) deferred.Entry {
	return deferred.Entry{ID: "r1", Source: "ph", Index: 1, Text: "routed idea", Target: target}
}

// createdTitled returns the create input with the given title.
func createdTitled(t *testing.T, f *faultBoard, title string) forge.IssueInput {
	t.Helper()
	for _, in := range f.created {
		if in.Title == title {
			return in
		}
	}
	t.Fatalf("no issue created titled %q (created %d)", title, len(f.created))
	return forge.IssueInput{}
}

func targetLabels(labels []string) []string {
	var out []string
	for _, l := range labels {
		if strings.HasPrefix(l, "dross/target:") {
			out = append(out, l)
		}
	}
	return out
}

// TestPushBacklogCreatesThenUpdatesByIDKey: the first push creates, the second
// finds each item by its id key and updates it; a re-route replaces the target
// label rather than adding to it.
func TestPushBacklogCreatesThenUpdatesByIDKey(t *testing.T) {
	f := newFaultBoard()
	ctx, _ := boardCtx(t, f)
	writeMilestoneToml(t, ctx.Root, "v1", "active", "")
	items := []BacklogItem{DeferredBacklogItem(plainEntry()), DeferredBacklogItem(routedEntry("x"))}

	var created, updated int
	var err error
	captureStderr(t, func() { created, updated, err = PushBacklogItems(ctx, "v1", items) })
	if err != nil || created != 2 || updated != 0 {
		t.Fatalf("first push = (%d, %d, %v), want (2, 0, nil)", created, updated, err)
	}
	routed := createdTitled(t, f, "[routed] routed idea")
	if !contains(routed.Labels, DeferredLabel("r1")) || !contains(routed.Labels, TargetLabel("x")) {
		t.Errorf("routed create labels = %v, want its identity and target labels", routed.Labels)
	}
	if routed.Milestone != 7 {
		t.Errorf("create Milestone = %d, want the board's milestone id 7", routed.Milestone)
	}
	if len(f.milestones) != 1 || f.milestones[0].Title != "v1" {
		t.Errorf("EnsureMilestone calls = %v, want one for v1", f.milestones)
	}
	plainKey, ok := ctx.Board.BacklogID(DeferredBacklogKey("p1"))
	if !ok {
		t.Fatal("the plain item's id key was not recorded")
	}

	captureStderr(t, func() { created, updated, err = PushBacklogItems(ctx, "v1", items) })
	if err != nil || created != 0 || updated != 2 {
		t.Fatalf("second push = (%d, %d, %v), want (0, 2, nil)", created, updated, err)
	}
	if len(f.created) != 2 {
		t.Errorf("the second push created again: %d creates total", len(f.created))
	}

	rerouted := []BacklogItem{DeferredBacklogItem(routedEntry("y"))}
	captureStderr(t, func() { created, updated, err = PushBacklogItems(ctx, "v1", rerouted) })
	if err != nil || created != 0 || updated != 1 {
		t.Fatalf("re-route push = (%d, %d, %v), want (0, 1, nil)", created, updated, err)
	}
	routedKey, _ := ctx.Board.BacklogID(DeferredBacklogKey("r1"))
	if got := targetLabels(f.issues[routedKey].Labels); !reflect.DeepEqual(got, []string{TargetLabel("y")}) {
		t.Errorf("target labels after re-route = %v, want only dross/target:y", got)
	}

	// An update carrying no labels leaves the issue's label set alone.
	before := append([]string(nil), f.issues[plainKey].Labels...)
	bare := BacklogItem{Key: DeferredBacklogKey("p1"), Title: "retitled", Body: "b"}
	if created, updated, err = PushBacklogItems(ctx, "v1", []BacklogItem{bare}); err != nil || created != 0 || updated != 1 {
		t.Fatalf("label-less update = (%d, %d, %v), want (0, 1, nil)", created, updated, err)
	}
	if got := f.issues[plainKey].Labels; !reflect.DeepEqual(got, before) {
		t.Errorf("a label-less update rewrote labels: %v, want %v", got, before)
	}
	if f.issues[plainKey].Title != "retitled" {
		t.Errorf("title = %q, want the update applied", f.issues[plainKey].Title)
	}

	// A label-less create still carries the marker.
	slug := BacklogItem{Key: "slug:z", Title: "[backlog] z", Body: "b"}
	if created, updated, err = PushBacklogItems(ctx, "v1", []BacklogItem{slug}); err != nil || created != 1 || updated != 0 {
		t.Fatalf("label-less create = (%d, %d, %v), want (1, 0, nil)", created, updated, err)
	}
	if got := createdTitled(t, f, "[backlog] z").Labels; !reflect.DeepEqual(got, []string{LabelMarker}) {
		t.Errorf("label-less create labels = %v, want [dross]", got)
	}
}

// TestResolveBacklogAdoptsMarkerBearingIdentityMatch: with no cached link, the
// tracker is asked by identity label and the lowest-keyed issue that also
// carries the marker is adopted; a hand-made issue with the label is not.
func TestResolveBacklogAdoptsMarkerBearingIdentityMatch(t *testing.T) {
	item := DeferredBacklogItem(plainEntry())

	f := newFaultBoard()
	f.seed(forge.Issue{Key: "PROJ-2", Title: "hand-made", Labels: []string{item.Identity}})
	f.seed(forge.Issue{Key: "PROJ-5", Title: "later", Labels: []string{LabelMarker, item.Identity}})
	f.seed(forge.Issue{Key: "PROJ-3", Title: "earlier", Labels: []string{LabelMarker, item.Identity}})
	ctx, _ := boardCtx(t, f)
	created, updated, err := PushBacklogItems(ctx, "v1", []BacklogItem{item})
	if err != nil || created != 0 || updated != 1 {
		t.Fatalf("push = (%d, %d, %v), want (0, 1, nil)", created, updated, err)
	}
	if got, _ := ctx.Board.BacklogID(item.Key); got != "PROJ-3" {
		t.Errorf("adopted %q, want the lowest marker-bearing key PROJ-3", got)
	}
	if !reflect.DeepEqual(f.updated, []string{"PROJ-3"}) {
		t.Errorf("updated %v, want only PROJ-3", f.updated)
	}

	unmarked := newFaultBoard()
	unmarked.seed(forge.Issue{Key: "PROJ-2", Title: "hand-made", Labels: []string{item.Identity}})
	ctx, _ = boardCtx(t, unmarked)
	if created, updated, err = PushBacklogItems(ctx, "v1", []BacklogItem{item}); err != nil || created != 1 || updated != 0 {
		t.Fatalf("unmarked-only push = (%d, %d, %v), want (1, 0, nil)", created, updated, err)
	}
	if got, _ := ctx.Board.BacklogID(item.Key); got == "PROJ-2" {
		t.Error("an issue without the dross marker was adopted")
	}

	broken := newFaultBoard()
	broken.listErr = errors.New("tracker down")
	ctx, _ = boardCtx(t, broken)
	created, updated, err = PushBacklogItems(ctx, "v1", []BacklogItem{item})
	if err == nil || !strings.HasPrefix(err.Error(), "board:") || created != 0 || updated != 0 {
		t.Errorf("list failure = (%d, %d, %v), want (0, 0, board: error)", created, updated, err)
	}
	if len(broken.callsOf("CreateIssue")) != 0 {
		t.Error("a list failure still created an issue")
	}
}

// TestLegacyBacklogKeyAdoptedOnlyOnTitleMatch: a pre-id positional link is
// migrated onto the id key only when the stored issue still carries the
// item's title; otherwise it is left alone and the item is created fresh.
func TestLegacyBacklogKeyAdoptedOnlyOnTitleMatch(t *testing.T) {
	item := BacklogItem{Key: "someday:id:a", LegacyKey: "someday:ph#0", Title: "[someday] t", Body: "b", Labels: []string{LabelMarker}}

	for _, tc := range []struct {
		name   string
		title  string
		fault  func(f *faultBoard)
		adopts bool
	}{
		{"title matches", "[someday] t", nil, true},
		{"title drifted", "[someday] someone else", nil, false},
		{"issue unreadable", "[someday] t", func(f *faultBoard) { f.failGet["PROJ-4"] = errors.New("boom") }, false},
		{"issue missing", "[someday] t", func(f *faultBoard) { f.nilGet["PROJ-4"] = true }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFaultBoard()
			f.seed(forge.Issue{Key: "PROJ-4", Title: tc.title, Labels: []string{LabelMarker}})
			if tc.fault != nil {
				tc.fault(f)
			}
			ctx, _ := boardCtx(t, f)
			ctx.Board.SetBacklog(item.LegacyKey, "PROJ-4")

			created, updated, err := PushBacklogItems(ctx, "v1", []BacklogItem{item})
			if err != nil {
				t.Fatal(err)
			}
			idKey, _ := ctx.Board.BacklogID(item.Key)
			legacy, legacyKept := ctx.Board.BacklogID(item.LegacyKey)
			if tc.adopts {
				if created != 0 || updated != 1 || idKey != "PROJ-4" || legacyKept {
					t.Errorf("(%d, %d) id=%q legacyKept=%v; want (0, 1) id=PROJ-4 and the legacy key gone", created, updated, idKey, legacyKept)
				}
				return
			}
			if created != 1 || updated != 0 || idKey == "PROJ-4" || legacy != "PROJ-4" {
				t.Errorf("(%d, %d) id=%q legacy=%q; want (1, 0), a fresh issue and the legacy link untouched", created, updated, idKey, legacy)
			}
		})
	}
}

// TestPushBacklogYouTrackVersionAndEpicModes: YouTrack creates through
// CreateBacklogItem. Version mode (and the unset default) carries the cached
// fix version on the create; epic mode sends none and links the new item as a
// subtask of the epic before tagging it.
func TestPushBacklogYouTrackVersionAndEpicModes(t *testing.T) {
	item := BacklogItem{Key: "slug:a", Title: "[backlog] a", Body: "b", Labels: []string{LabelMarker}}
	scriptCreateAndTag := func(tr *scriptedTracker) {
		tr.on("POST", "/api/issues", `{"idReadable":"PROJ-1"}`)
		tr.on("GET", "/api/issues/PROJ-1", `{"idReadable":"PROJ-1","tags":[]}`)
		tr.on("GET", "/api/issueTags", `[{"id":"t1","name":"dross"}]`)
		tr.on("POST", "/api/issues/PROJ-1/tags", `{}`)
	}

	for _, mode := range []string{"version", ""} {
		t.Run("mode "+mode, func(t *testing.T) {
			tr := strictTracker(t)
			scriptCreateAndTag(tr)
			_, ctx := ytRepoMode(t, tr, `{"milestones":{"v1":"0.1"}}`, mode)
			created, updated, err := PushBacklogItems(ctx, "v1", []BacklogItem{item})
			if err != nil || created != 1 || updated != 0 {
				t.Fatalf("push = (%d, %d, %v), want (1, 0, nil)", created, updated, err)
			}
			posts := tr.callsTo("POST", "/api/issues")
			if len(posts) != 1 || !strings.Contains(posts[0].Body, `"customFields"`) || !strings.Contains(posts[0].Body, `"name":"0.1"`) {
				t.Errorf("create body = %v, want the fix version 0.1", posts)
			}
			if got, _ := ctx.Board.BacklogID(item.Key); got != "PROJ-1" {
				t.Errorf("recorded %q, want PROJ-1", got)
			}
		})
	}

	t.Run("mode epic", func(t *testing.T) {
		tr := strictTracker(t)
		scriptCreateAndTag(tr)
		tr.on("POST", "/api/commands", `{}`)
		_, ctx := ytRepoMode(t, tr, `{"milestones":{"v1":"PROJ-100"}}`, "epic")
		created, updated, err := PushBacklogItems(ctx, "v1", []BacklogItem{item})
		if err != nil || created != 1 || updated != 0 {
			t.Fatalf("push = (%d, %d, %v), want (1, 0, nil)", created, updated, err)
		}
		if posts := tr.callsTo("POST", "/api/issues"); len(posts) != 1 || strings.Contains(posts[0].Body, "customFields") {
			t.Errorf("epic-mode create body = %v, want no fix version", posts)
		}
		cmds := tr.callsTo("POST", "/api/commands")
		if len(cmds) != 1 || !strings.Contains(cmds[0].Body, "subtask of PROJ-100") || !strings.Contains(cmds[0].Body, `"idReadable":"PROJ-1"`) {
			t.Errorf("commands = %v, want PROJ-1 made a subtask of PROJ-100", cmds)
		}
		var order []string
		for _, c := range tr.calls {
			order = append(order, c.Method+" "+c.Path)
		}
		create, link, tag := indexOf(order, "POST /api/issues"), indexOf(order, "POST /api/commands"), indexOf(order, "GET /api/issues/PROJ-1")
		if !(create >= 0 && create < link && link < tag) {
			t.Errorf("request order = %v, want create, then subtask link, then the tag patch", order)
		}
	})

	t.Run("epic with no entity links nothing", func(t *testing.T) {
		tr := strictTracker(t)
		scriptCreateAndTag(tr)
		_, ctx := ytRepoMode(t, tr, `{}`, "epic")
		if created, _, err := PushBacklogItems(ctx, "v1", []BacklogItem{item}); err != nil || created != 1 {
			t.Fatalf("push = (%d, %v), want 1 created", created, err)
		}
	})

	t.Run("failed create", func(t *testing.T) {
		tr := strictTracker(t)
		tr.fail("POST", "/api/issues")
		_, ctx := ytRepoMode(t, tr, `{"milestones":{"v1":"PROJ-100"}}`, "epic")
		created, updated, err := PushBacklogItems(ctx, "v1", []BacklogItem{item})
		if err == nil || created != 0 || updated != 0 {
			t.Errorf("push = (%d, %d, %v), want (0, 0, error)", created, updated, err)
		}
		if _, ok := ctx.Board.BacklogID(item.Key); ok {
			t.Error("a failed create recorded a link")
		}
	})

	t.Run("failed subtask link", func(t *testing.T) {
		tr := strictTracker(t)
		tr.on("POST", "/api/issues", `{"idReadable":"PROJ-1"}`)
		tr.fail("POST", "/api/commands")
		_, ctx := ytRepoMode(t, tr, `{"milestones":{"v1":"PROJ-100"}}`, "epic")
		created, updated, err := PushBacklogItems(ctx, "v1", []BacklogItem{item})
		if err == nil || !strings.HasPrefix(err.Error(), "board:") || created != 0 || updated != 0 {
			t.Errorf("push = (%d, %d, %v), want (0, 0, board: error)", created, updated, err)
		}
		if _, ok := ctx.Board.BacklogID(item.Key); ok {
			t.Error("a failed subtask link recorded a link")
		}
	})
}

func indexOf(ss []string, s string) int {
	for i, x := range ss {
		if x == s {
			return i
		}
	}
	return -1
}

func warningLines(stderr, needle string) int {
	n := 0
	for _, l := range strings.Split(stderr, "\n") {
		if strings.Contains(l, needle) {
			n++
		}
	}
	return n
}

// TestLinkRoutedWarnsOncePerRunWithoutLinker: a board that cannot link issues
// warns once per run, not once per routed item; a board that can links each
// routed item to its target's issue, and a missing target or a failed link
// only warns — the item itself is the deliverable.
func TestLinkRoutedWarnsOncePerRunWithoutLinker(t *testing.T) {
	two := []BacklogItem{
		DeferredBacklogItem(routedEntry("x")),
		DeferredBacklogItem(deferred.Entry{ID: "r2", Source: "ph", Index: 2, Text: "second", Target: "x"}),
		DeferredBacklogItem(plainEntry()),
	}
	flat := newFaultBoard()
	ctx, _ := boardCtx(t, flat)
	stderr := captureStderr(t, func() {
		if _, _, err := PushBacklogItems(ctx, "v1", two); err != nil {
			t.Fatal(err)
		}
	})
	if n := warningLines(stderr, "cannot link issues"); n != 1 {
		t.Errorf("%d no-linker warnings, want exactly 1:\n%s", n, stderr)
	}

	routed := DeferredBacklogItem(routedEntry("x"))

	l := newLinkBoard()
	ctx, _ = boardCtx(t, l)
	stderr = captureStderr(t, func() {
		if _, _, err := PushBacklogItems(ctx, "v1", []BacklogItem{routed, DeferredBacklogItem(plainEntry())}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stderr, `phase "x" has no board issue yet`) || len(l.callsOf("LinkIssues")) != 0 {
		t.Errorf("no target issue: stderr %q, links %v; want a warning naming x and no LinkIssues", stderr, l.callsOf("LinkIssues"))
	}
	if warningLines(stderr, "warning:") != 1 {
		t.Errorf("want exactly one warning (the unrouted item warns nothing):\n%s", stderr)
	}

	l = newLinkBoard()
	ctx, _ = boardCtx(t, l)
	ctx.Board.SetPhase("x", "PROJ-50")
	stderr = captureStderr(t, func() {
		if _, _, err := PushBacklogItems(ctx, "v1", []BacklogItem{routed}); err != nil {
			t.Fatal(err)
		}
	})
	itemKey, _ := ctx.Board.BacklogID(routed.Key)
	if !reflect.DeepEqual(l.links, [][2]string{{itemKey, "PROJ-50"}}) || stderr != "" {
		t.Errorf("links = %v, stderr %q; want [%s PROJ-50] and no warning", l.links, stderr, itemKey)
	}

	l = newLinkBoard()
	l.linkErr = errors.New("no link type")
	ctx, _ = boardCtx(t, l)
	ctx.Board.SetPhase("x", "PROJ-50")
	var created int
	stderr = captureStderr(t, func() {
		var err error
		if created, _, err = PushBacklogItems(ctx, "v1", []BacklogItem{routed}); err != nil {
			t.Fatal(err)
		}
	})
	if created != 1 || !strings.Contains(stderr, "could not link") {
		t.Errorf("failed link: created %d, stderr %q; want the item counted and a warning", created, stderr)
	}
}

// TestLookupPhaseIssueNeverCreates: the link-target lookup reads the cache,
// then the tracker by phase label (marker-filtered, lowest key), and returns ""
// rather than ever minting the issue it wanted to point at.
func TestLookupPhaseIssueNeverCreates(t *testing.T) {
	f := newFaultBoard()
	ctx, _ := boardCtx(t, f)
	ctx.Board.SetPhase("cached", "PROJ-9")
	if got := lookupPhaseIssue(ctx, "cached"); got != "PROJ-9" || len(f.callsOf("ListIssues")) != 0 {
		t.Errorf("cache hit = %q with %d list calls, want PROJ-9 and none", got, len(f.callsOf("ListIssues")))
	}

	f.seed(forge.Issue{Key: "PROJ-2", Labels: []string{PhaseLabel("p")}})
	f.seed(forge.Issue{Key: "PROJ-6", Labels: []string{LabelMarker, PhaseLabel("p")}})
	f.seed(forge.Issue{Key: "PROJ-4", Labels: []string{LabelMarker, PhaseLabel("p")}})
	if got := lookupPhaseIssue(ctx, "p"); got != "PROJ-4" {
		t.Errorf("label query = %q, want the lowest marker-bearing key PROJ-4", got)
	}
	if got := lookupPhaseIssue(ctx, "nobody"); got != "" {
		t.Errorf("no match = %q, want \"\"", got)
	}
	f.listErr = errors.New("down")
	if got := lookupPhaseIssue(ctx, "p"); got != "" {
		t.Errorf("list error = %q, want \"\"", got)
	}
	if n := len(f.callsOf("CreateIssue")); n != 0 {
		t.Errorf("lookupPhaseIssue created %d issues", n)
	}
}

// TestDeferredBacklogItemShape pins the rendered item exactly: backlog-sync and
// `deferred add` share it, so any drift re-titles live issues.
func TestDeferredBacklogItemShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   deferred.Entry
		want BacklogItem
	}{
		{"with id", deferred.Entry{ID: "a", Source: "ph", Index: 2, Text: "t"}, BacklogItem{
			Key: "someday:id:a", LegacyKey: "someday:ph#2", Title: "[someday] t",
			Body:     "Someday idea (from phase `ph`): t\n\n_Tracked by dross._",
			Labels:   []string{LabelMarker, "dross/deferred:a"},
			Identity: "dross/deferred:a",
		}},
		{"id-less", deferred.Entry{Source: "ph", Index: 0, Text: "t"}, BacklogItem{
			Key: "someday:id:", LegacyKey: "someday:ph#0", Title: "[someday] t",
			Body:   "Someday idea (from phase `ph`): t\n\n_Tracked by dross._",
			Labels: []string{LabelMarker},
		}},
		{"routed", deferred.Entry{ID: "r", Source: "ph", Index: 1, Text: "go", Target: "x"}, BacklogItem{
			Key: "someday:id:r", LegacyKey: "someday:ph#1", Title: "[routed] go",
			Body:     "Deferred item routed to `x` (from phase `ph`): go\n\n_Tracked by dross._",
			Labels:   []string{LabelMarker, "dross/deferred:r", "dross/target:x"},
			Identity: "dross/deferred:r",
			Target:   "x",
		}},
	} {
		if got := DeferredBacklogItem(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
	}
}

// TestPushBacklogFailurePaths: every failure returns the counts reached so far
// and an error, never a silent partial success.
func TestPushBacklogFailurePaths(t *testing.T) {
	t.Run("milestone error aborts before any create", func(t *testing.T) {
		f := newFaultBoard()
		f.milestoneErr = errors.New("no milestones here")
		ctx, _ := boardCtx(t, f)
		writeMilestoneToml(t, ctx.Root, "v1", "active", "")
		created, updated, err := PushBacklogItems(ctx, "v1", []BacklogItem{DeferredBacklogItem(plainEntry())})
		if err == nil || created != 0 || updated != 0 || len(f.callsOf("CreateIssue")) != 0 {
			t.Errorf("(%d, %d, %v) with %d creates; want (0, 0, error) and none", created, updated, err, len(f.callsOf("CreateIssue")))
		}
	})

	t.Run("failed update", func(t *testing.T) {
		f := newFaultBoard()
		f.seed(forge.Issue{Key: "PROJ-1", Labels: []string{LabelMarker}})
		f.failUpdate["PROJ-1"] = errors.New("refused")
		ctx, _ := boardCtx(t, f)
		item := DeferredBacklogItem(plainEntry())
		ctx.Board.SetBacklog(item.Key, "PROJ-1")
		created, updated, err := PushBacklogItems(ctx, "v1", []BacklogItem{item})
		if err == nil || !strings.HasPrefix(err.Error(), "board:") || created != 0 || updated != 0 {
			t.Errorf("(%d, %d, %v), want (0, 0, board: error)", created, updated, err)
		}
	})

	t.Run("failed create after one success", func(t *testing.T) {
		f := newFaultBoard()
		f.failCreate["[someday] bad"] = errors.New("refused")
		ctx, _ := boardCtx(t, f)
		items := []BacklogItem{
			DeferredBacklogItem(plainEntry()),
			DeferredBacklogItem(deferred.Entry{ID: "b", Source: "ph", Index: 3, Text: "bad"}),
		}
		created, updated, err := PushBacklogItems(ctx, "v1", items)
		if err == nil || !strings.HasPrefix(err.Error(), "board:") || created != 1 || updated != 0 {
			t.Errorf("(%d, %d, %v), want (1, 0, board: error)", created, updated, err)
		}
	})

	t.Run("unwritable board path", func(t *testing.T) {
		f := newFaultBoard()
		ctx, _ := boardCtx(t, f)
		ctx.BoardPath = filepath.Join(t.TempDir(), "missing", "board.json")
		created, updated, err := PushBacklogItems(ctx, "v1", []BacklogItem{DeferredBacklogItem(plainEntry())})
		if err == nil || !strings.Contains(err.Error(), "board.json") || created != 1 || updated != 0 {
			t.Errorf("(%d, %d, %v), want (1, 0, the save error)", created, updated, err)
		}
	})
}
