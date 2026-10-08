package boardsync

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/phase"
)

// Survivors routed to this phase from outside the six c-2 files: the phase and
// task sync paths, task-pull's provider refusal, and the quick-mirror lane.

const authSpec = "[phase]\nid = \"auth\"\ntitle = \"Auth\"\n"

const twoTaskPlan = `[phase]
id = "auth"

[[task]]
id = "t-1"
wave = 1
title = "one"
files = ["a.go"]
covers = ["c-1"]
status = "pending"

[[task]]
id = "t-2"
wave = 1
title = "two"
files = ["b.go"]
covers = ["c-1"]
status = "pending"
`

// phaseRepo writes the auth phase's spec (and plan, when given) under ctx's
// root.
func phaseRepo(t *testing.T, ctx *Ctx, spec, plan string) {
	t.Helper()
	dir := filepath.Join(ctx.Root, "phases", "auth")
	mustWrite(t, filepath.Join(dir, "spec.toml"), spec)
	if plan != "" {
		mustWrite(t, filepath.Join(dir, "plan.toml"), plan)
	}
}

// TestPhaseSyncAdoptsLegacyTitleOnlyWithMarker: an issue from before phase
// labels existed is adopted by its exact summary, but only if dross's marker
// says it is dross's.
func TestPhaseSyncAdoptsLegacyTitleOnlyWithMarker(t *testing.T) {
	f := newFaultBoard()
	f.seed(forge.Issue{Key: "PROJ-4", Title: "auth — Auth", Labels: []string{LabelMarker}})
	ctx, _ := boardCtx(t, f)
	phaseRepo(t, ctx, authSpec, "")
	if err := SyncPhase(ctx, "auth", "", false); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 0 || !reflect.DeepEqual(f.updated, []string{"PROJ-4"}) {
		t.Errorf("created %d, updated %v; want the legacy PROJ-4 adopted", len(f.created), f.updated)
	}

	g := newFaultBoard()
	g.seed(forge.Issue{Key: "PROJ-4", Title: "auth — Auth"})
	gctx, _ := boardCtx(t, g)
	phaseRepo(t, gctx, authSpec, "")
	if err := SyncPhase(gctx, "auth", "", false); err != nil {
		t.Fatal(err)
	}
	if len(g.created) != 1 || len(g.updated) != 0 {
		t.Errorf("created %d, updated %v; want a new issue beside the unmarked one", len(g.created), g.updated)
	}
}

// TestPhaseSyncAssignsDeclaredMilestone: a spec's milestone is ensured on the
// board and carried on the create; a spec without one never asks.
func TestPhaseSyncAssignsDeclaredMilestone(t *testing.T) {
	f := newFaultBoard()
	ctx, _ := boardCtx(t, f)
	phaseRepo(t, ctx, "[phase]\nid = \"auth\"\ntitle = \"Auth\"\nmilestone = \"v1\"\n", "")
	writeMilestoneScope(t, ctx.Root, "v1", "Launch")
	if err := SyncPhase(ctx, "auth", "", false); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 1 || f.created[0].Milestone != 7 {
		t.Fatalf("created %+v, want one issue on milestone 7", f.created)
	}
	if id, _ := ctx.Board.MilestoneID("v1"); id != "7" {
		t.Errorf("board.json milestone v1 -> %q, want 7", id)
	}

	g := newFaultBoard()
	gctx, _ := boardCtx(t, g)
	phaseRepo(t, gctx, authSpec, "")
	if err := SyncPhase(gctx, "auth", "", false); err != nil {
		t.Fatal(err)
	}
	if len(g.created) != 1 || g.created[0].Milestone != 0 || len(g.milestones) != 0 {
		t.Errorf("created %+v with %d milestone calls, want milestone 0 and none", g.created, len(g.milestones))
	}
}

// ytCreateScript scripts a YouTrack create of key through the tag writes that
// follow it, for a board whose tag index knows only the marker. A phase sync
// also makes the legacy marker query (GET /api/issues); a task sync does not,
// so that route is scripted by the phase subtests alone.
func ytCreateScript(tr *scriptedTracker, key string) {
	tr.on("GET", "/api/issueTags", `[{"id":"t0","name":"dross"}]`)
	tr.on("POST", "/api/issues", `{"idReadable":"`+key+`"}`)
	tr.on("GET", "/api/issues/"+key, `{"idReadable":"`+key+`","tags":[]}`)
	tr.on("POST", "/api/issueTags", `{"id":"tn"}`)
	tr.on("POST", "/api/issues/"+key+"/tags", `{}`)
}

// ytStateBundle answers the bundle lookup SetState makes after a refused
// write, with value already present — so the refusal is reported rather than
// retried after adding it.
func ytStateBundle(tr *scriptedTracker, value string) {
	tr.on("GET", "/api/admin/projects/PROJ/customFields",
		`[{"field":{"name":"State"},"bundle":{"id":"b1","values":[{"name":"`+value+`"}],"projects":[{"shortName":"PROJ"}]}}]`)
}

// TestPhaseSyncSurfacesTrackerStateWriteFailure: a state write the tracker
// refuses fails the sync with a board: error — for the phase sync, the verified
// close and the task sync alike — and a successful one saves the link.
func TestPhaseSyncSurfacesTrackerStateWriteFailure(t *testing.T) {
	t.Run("youtrack phase sync", func(t *testing.T) {
		tr := strictTracker(t)
		ytCreateScript(tr, "PROJ-1")
		tr.on("GET", "/api/issues", `[]`)
		tr.fail("POST", "/api/issues/PROJ-1")
		ytStateBundle(tr, "Open")
		_, ctx := ytRepoMode(t, tr, `{}`, "epic")
		phaseRepo(t, ctx, authSpec, "")
		var err error
		captureStderr(t, func() { err = SyncPhase(ctx, "auth", "", false) })
		if err == nil || !strings.HasPrefix(err.Error(), "board:") {
			t.Errorf("err = %v, want a board: error", err)
		}
	})

	t.Run("youtrack phase sync success saves the link", func(t *testing.T) {
		tr := strictTracker(t)
		ytCreateScript(tr, "PROJ-1")
		tr.on("GET", "/api/issues", `[]`)
		tr.on("POST", "/api/issues/PROJ-1", `{}`)
		_, ctx := ytRepoMode(t, tr, `{}`, "epic")
		phaseRepo(t, ctx, authSpec, "")
		var err error
		captureStderr(t, func() { err = SyncPhase(ctx, "auth", "", false) })
		if err != nil {
			t.Fatal(err)
		}
		saved, err := board.Load(ctx.BoardPath)
		if err != nil {
			t.Fatal(err)
		}
		if key, _ := saved.PhaseIssue("auth"); key != "PROJ-1" {
			t.Errorf("saved phase link %q, want PROJ-1", key)
		}
	})

	t.Run("youtrack close", func(t *testing.T) {
		tr := strictTracker(t)
		tr.fail("POST", "/api/issues/PROJ-1")
		_, ctx := ytRepoMode(t, tr, `{}`, "epic")
		if err := CloseIssue(ctx, "PROJ-1", ""); err == nil || !strings.HasPrefix(err.Error(), "board:") {
			t.Errorf("err = %v, want a board: error", err)
		}
	})

	t.Run("youtrack task sync", func(t *testing.T) {
		tr := strictTracker(t)
		ytCreateScript(tr, "PROJ-2")
		tr.fail("POST", "/api/issues/PROJ-2")
		ytStateBundle(tr, "In Progress")
		_, ctx := ytRepoMode(t, tr, `{"phases":{"auth":"PROJ-1"}}`, "epic")
		phaseRepo(t, ctx, authSpec, strings.SplitN(twoTaskPlan, "[[task]]\nid = \"t-2\"", 2)[0])
		var err error
		captureStderr(t, func() { err = SyncTasks(ctx, "auth", "", StatusTaskInProgress, false) })
		if err == nil || !strings.HasPrefix(err.Error(), "board:") {
			t.Errorf("err = %v, want a board: error", err)
		}
	})

	t.Run("youtrack task sync without a status writes no state", func(t *testing.T) {
		tr := strictTracker(t)
		ytCreateScript(tr, "PROJ-2")
		tr.on("POST", "/api/commands", `{}`)
		_, ctx := ytRepoMode(t, tr, `{"phases":{"auth":"PROJ-1"}}`, "epic")
		phaseRepo(t, ctx, authSpec, strings.SplitN(twoTaskPlan, "[[task]]\nid = \"t-2\"", 2)[0])
		var err error
		captureStderr(t, func() { err = SyncTasks(ctx, "auth", "", "", false) })
		if err != nil {
			t.Fatal(err)
		}
		// No POST /api/issues/PROJ-2 is scripted: a state write would be an
		// unscripted request and fail the strict tracker at cleanup.
	})

	jiraTransitions := func(tr *scriptedTracker, key, id, to string) {
		tr.on("GET", "/rest/api/3/issue/"+key+"/transitions", `{"transitions":[{"id":"`+id+`","to":{"name":"`+to+`"}}]}`)
		tr.fail("POST", "/rest/api/3/issue/"+key+"/transitions")
	}

	t.Run("jira phase sync", func(t *testing.T) {
		tr := strictTracker(t)
		tr.on("GET", "/rest/api/3/label", `{"values":[]}`)
		tr.on("POST", "/rest/api/3/issue", `{"key":"PROJ-1"}`)
		jiraTransitions(tr, "PROJ-1", "11", "To Do")
		_, ctx := jiraRepo(t, tr, `{}`)
		phaseRepo(t, ctx, authSpec, "")
		var err error
		captureStderr(t, func() { err = SyncPhase(ctx, "auth", "", false) })
		if err == nil || !strings.HasPrefix(err.Error(), "board:") {
			t.Errorf("err = %v, want a board: error", err)
		}
	})

	t.Run("jira close refuses before any read-back", func(t *testing.T) {
		tr := strictTracker(t)
		jiraTransitions(tr, "PROJ-1", "31", "Done")
		_, ctx := jiraRepo(t, tr, `{}`)
		if err := CloseIssue(ctx, "PROJ-1", ""); err == nil || !strings.HasPrefix(err.Error(), "board:") {
			t.Errorf("err = %v, want a board: error", err)
		}
		// GET /rest/api/3/issue/PROJ-1 is unscripted: a read-back after the
		// refused transition would fail the strict tracker at cleanup.
	})

	t.Run("jira task sync", func(t *testing.T) {
		tr := strictTracker(t)
		tr.on("GET", "/rest/api/3/label", `{"values":[]}`)
		tr.on("POST", "/rest/api/3/issue", `{"key":"PROJ-2"}`)
		jiraTransitions(tr, "PROJ-2", "21", "In Progress")
		_, ctx := jiraRepo(t, tr, `{"phases":{"auth":"PROJ-1"}}`)
		phaseRepo(t, ctx, authSpec, strings.SplitN(twoTaskPlan, "[[task]]\nid = \"t-2\"", 2)[0])
		var err error
		captureStderr(t, func() { err = SyncTasks(ctx, "auth", "", StatusTaskInProgress, false) })
		if err == nil || !strings.HasPrefix(err.Error(), "board:") {
			t.Errorf("err = %v, want a board: error", err)
		}
	})
}

// TestTaskSyncLinkFailureWarnsOnce: a tracker that reports it cannot relate
// issues is warned about once per run, and every task still gets its issue.
func TestTaskSyncLinkFailureWarnsOnce(t *testing.T) {
	l := newLinkBoard()
	l.linkErr = errors.New("no link type")
	ctx, _ := boardCtx(t, l)
	ctx.Board.SetPhase("auth", "PROJ-100")
	phaseRepo(t, ctx, authSpec, twoTaskPlan)
	var err error
	stderr := captureStderr(t, func() { err = SyncTasks(ctx, "auth", "", "", false) })
	if err != nil {
		t.Fatal(err)
	}
	if n := warningLines(stderr, "could not relate task issues"); n != 1 {
		t.Errorf("%d link warnings, want exactly 1:\n%s", n, stderr)
	}
	if len(l.created) != 2 {
		t.Errorf("created %d task issues, want 2", len(l.created))
	}
}

// TestTaskIssueResolutionWarnsOnDuplicates: two dross issues on one task label
// are named, and the lowest key is the one updated; one match is silent.
func TestTaskIssueResolutionWarnsOnDuplicates(t *testing.T) {
	plan := strings.SplitN(twoTaskPlan, "[[task]]\nid = \"t-2\"", 2)[0]
	labels := []string{LabelMarker, TaskLabel("auth", "t-1")}

	f := newFaultBoard()
	f.seed(forge.Issue{Key: "PROJ-5", Labels: labels})
	f.seed(forge.Issue{Key: "PROJ-3", Labels: labels})
	ctx, _ := boardCtx(t, f)
	ctx.Board.SetPhase("auth", "PROJ-100")
	phaseRepo(t, ctx, authSpec, plan)
	stderr := captureStderr(t, func() {
		if err := SyncTasks(ctx, "auth", "", "", false); err != nil {
			t.Fatal(err)
		}
	})
	if !reflect.DeepEqual(f.updated, []string{"PROJ-3"}) {
		t.Errorf("updated %v, want the lowest key PROJ-3", f.updated)
	}
	dup := "issues carry " + TaskLabel("auth", "t-1") + " ("
	if !strings.Contains(stderr, "2 "+dup+"PROJ-3, PROJ-5)") {
		t.Errorf("stderr does not name both duplicates:\n%s", stderr)
	}

	g := newFaultBoard()
	g.seed(forge.Issue{Key: "PROJ-3", Labels: labels})
	gctx, _ := boardCtx(t, g)
	gctx.Board.SetPhase("auth", "PROJ-100")
	phaseRepo(t, gctx, authSpec, plan)
	stderr = captureStderr(t, func() {
		if err := SyncTasks(gctx, "auth", "", "", false); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(stderr, dup) {
		t.Errorf("a single match warned:\n%s", stderr)
	}
}

// TestTaskPullRefusesFlatBoards: a board with no workflow state is refused by
// name before anything is read, and the plan is left byte-identical.
func TestTaskPullRefusesFlatBoards(t *testing.T) {
	f := newFaultBoard()
	ctx, _ := boardCtx(t, f)
	ctx.Proj.Board.Provider = "github"
	phaseRepo(t, ctx, authSpec, twoTaskPlan)
	planPath := filepath.Join(ctx.Root, "phases", "auth", "plan.toml")
	before := mustRead(t, planPath)

	err := TaskPull(ctx, "auth", nil, planPath, true)
	if err == nil {
		t.Fatal("a flat board was not refused")
	}
	for _, want := range []string{`"github"`, "has no workflow state", "needs youtrack or jira"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not contain %q", err, want)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("board calls %v before the refusal", f.calls)
	}
	assertPlanUnchanged(t, planPath, before)
}

// TestTaskPullSurfacesListFailure: a task-pull whose board read fails returns
// the error and applies nothing. The read that fails is the per-task
// GetIssue — CollectTaskMoves reads each linked card rather than listing.
func TestTaskPullSurfacesListFailure(t *testing.T) {
	f := newFaultBoard()
	f.failGet["PROJ-7"] = errors.New("tracker down")
	ctx, _ := boardCtx(t, f)
	ctx.Proj.Board.Provider = "youtrack"
	ctx.Board.SetTask("auth", "t-1", "PROJ-7")
	phaseRepo(t, ctx, authSpec, twoTaskPlan)
	planPath := filepath.Join(ctx.Root, "phases", "auth", "plan.toml")
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, planPath)

	if err := TaskPull(ctx, "auth", plan, planPath, true); err == nil || !strings.Contains(err.Error(), "tracker down") {
		t.Errorf("err = %v, want the read failure", err)
	}
	assertPlanUnchanged(t, planPath, before)
}

// TestQuickMirrorWithEmptyIssueIsSkipped: a quick with no issue id has no card
// to name.
func TestQuickMirrorWithEmptyIssueIsSkipped(t *testing.T) {
	ctx, _ := boardCtx(t, newFaultBoard())
	ctx.Board.SetQuick("q1", "")
	ctx.Board.SetQuick("q2", "P-9")
	var lane ReapLane
	for _, l := range ReapLanes {
		if l.Name == "Quicks" {
			lane = l
		}
	}
	got := classifyQuickMirrors(ctx, lane)
	if len(got) != 1 || got[0].card.Key != "P-9" || !strings.Contains(got[0].card.Why, "q2") {
		t.Errorf("quick mirrors = %+v, want only P-9, naming q2", got)
	}
	if got[0].verdict != ReapUnattributable {
		t.Errorf("verdict = %v, want unattributable", got[0].verdict)
	}
}
