package boardsync

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/forge"
)

// TestCollectInboundDropsDrossAuthoredIssues: the inbound feed is what humans
// filed. An issue dross links in any board.json namespace, one dismissed in
// triage, and one carrying the marker label are each dropped by their own
// check — so each row below fails if that check goes.
func TestCollectInboundDropsDrossAuthoredIssues(t *testing.T) {
	f := newFaultBoard()
	for _, iss := range []forge.Issue{
		{Key: "PROJ-1", Title: "human one"},
		{Key: "PROJ-2", Title: "human two", Labels: []string{"bug"}},
		{Key: "PROJ-3", Title: "phase mirror"},
		{Key: "PROJ-4", Title: "quick mirror"},
		{Key: "PROJ-5", Title: "backlog mirror"},
		{Key: "PROJ-6", Title: "task mirror"},
		{Key: "PROJ-7", Title: "epic mirror"},
		{Key: "PROJ-8", Title: "dismissed in triage"},
		{Key: "PROJ-9", Title: "unlinked dross issue", Labels: []string{LabelMarker}},
	} {
		f.seed(iss)
	}
	ctx, _ := boardCtx(t, f)
	ctx.Board.SetPhase("p", "PROJ-3")
	ctx.Board.SetQuick("q", "PROJ-4")
	ctx.Board.SetBacklog("slug:s", "PROJ-5")
	ctx.Board.SetTask("p", "t-1", "PROJ-6")
	ctx.Board.SetMilestone("v1", "PROJ-7")
	ctx.Board.Dismiss("PROJ-8")

	got, err := CollectInbound(ctx, forge.IssueFilter{State: "open"})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, iss := range got {
		keys = append(keys, iss.Key)
	}
	if want := []string{"PROJ-1", "PROJ-2"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("inbound = %v, want only the human-filed %v", keys, want)
	}

	f.listErr = errors.New("tracker down")
	got, err = CollectInbound(ctx, forge.IssueFilter{State: "open"})
	if err == nil || !strings.HasPrefix(err.Error(), "board:") || got != nil {
		t.Errorf("list failure = (%v, %v), want (nil, board: error)", got, err)
	}
}

// TestPullEnvelopeShape: the --json envelope never carries a null issue list,
// and carries the board error by message.
func TestPullEnvelopeShape(t *testing.T) {
	var b bytes.Buffer
	if err := EmitPullEnvelope(&b, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "{\"issues\":[],\"error\":null}\n" {
		t.Errorf("empty envelope = %q", got)
	}

	b.Reset()
	if err := EmitPullEnvelope(&b, nil, errors.New("tracker down")); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "{\"issues\":[],\"error\":\"tracker down\"}\n" {
		t.Errorf("error envelope = %q", got)
	}

	b.Reset()
	if err := EmitPullEnvelope(&b, []forge.Issue{{Key: "PROJ-1", Title: "one"}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); !strings.HasPrefix(got, `{"issues":[{`) || !strings.Contains(got, `"Key":"PROJ-1"`) || !strings.HasSuffix(got, "],\"error\":null}\n") {
		t.Errorf("one-issue envelope = %q", got)
	}
}

// TestReportBoardFailureByMode: --json always answers with the envelope and a
// nil error; a human caller gets the error back when it is fatal and a
// printed note when it is not.
func TestReportBoardFailureByMode(t *testing.T) {
	boom := errors.New("tracker down")

	var b bytes.Buffer
	if err := ReportBoardFailure(&b, true, true, boom); err != nil {
		t.Errorf("json mode returned %v, want nil", err)
	}
	if got := b.String(); got != "{\"issues\":[],\"error\":\"tracker down\"}\n" {
		t.Errorf("json mode printed %q", got)
	}

	b.Reset()
	if err := ReportBoardFailure(&b, false, true, boom); !errors.Is(err, boom) || b.Len() != 0 {
		t.Errorf("human fatal = %v, printed %q; want the error and no output", err, b.String())
	}

	b.Reset()
	if err := ReportBoardFailure(&b, false, false, boom); err != nil || b.String() != "board unreachable: tracker down\n" {
		t.Errorf("human non-fatal = %v, printed %q", err, b.String())
	}
}
