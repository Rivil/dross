package boardsync

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/project"
)

// fakeBoard is an in-memory forge.BoardClient that records every write, so a
// test can assert on the SHAPE of a sync — what was created, what was
// patched — rather than only on its outcome.
type fakeBoard struct {
	next    int
	issues  map[string]*forge.Issue
	created []forge.IssueInput
	updated []string
	closed  []string
}

func newFakeBoard() *fakeBoard { return &fakeBoard{issues: map[string]*forge.Issue{}} }

func (f *fakeBoard) EnsureMilestone(title, _ string) (string, error) { return "7", nil }

func (f *fakeBoard) CreateIssue(in forge.IssueInput) (*forge.Issue, error) {
	f.next++
	key := "PROJ-" + itoa(f.next)
	iss := &forge.Issue{Number: f.next, Key: key, Title: in.Title, Body: in.Body, State: "open", Labels: append([]string(nil), in.Labels...)}
	f.issues[key] = iss
	f.created = append(f.created, in)
	return iss, nil
}

func (f *fakeBoard) GetIssue(key string) (*forge.Issue, error) {
	iss, ok := f.issues[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	cp := *iss
	return &cp, nil
}

func (f *fakeBoard) UpdateIssue(key string, patch forge.IssuePatch) (*forge.Issue, error) {
	iss, ok := f.issues[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	if patch.Title != nil {
		iss.Title = *patch.Title
	}
	if patch.Body != nil {
		iss.Body = *patch.Body
	}
	if patch.Labels != nil {
		iss.Labels = append([]string(nil), (*patch.Labels)...)
	}
	if patch.State != nil {
		iss.State = *patch.State
	}
	f.updated = append(f.updated, key)
	cp := *iss
	return &cp, nil
}

func (f *fakeBoard) CloseIssue(key string) error {
	iss, ok := f.issues[key]
	if !ok {
		return os.ErrNotExist
	}
	iss.State = "closed"
	f.closed = append(f.closed, key)
	return nil
}

func (f *fakeBoard) ListIssues(filter forge.IssueFilter) ([]forge.Issue, error) {
	var out []forge.Issue
	for _, iss := range f.issues {
		if filter.State != "all" && filter.State != "" && iss.State != filter.State {
			continue
		}
		hit := true
		for _, want := range filter.Labels {
			found := false
			for _, l := range iss.Labels {
				if l == want {
					found = true
				}
			}
			if !found {
				hit = false
			}
		}
		if hit {
			out = append(out, *iss)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// syncFixture builds a .dross root holding one phase (spec + a plan with one
// pending task) and returns a Ctx over a fake board narrating into out.
func syncFixture(t *testing.T, out *bytes.Buffer) (*Ctx, *fakeBoard) {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".dross")
	dir := filepath.Join(root, "phases", "auth")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("spec.toml", "[phase]\nid = \"auth\"\ntitle = \"Auth\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	write("plan.toml", "[phase]\nid = \"auth\"\n\n[[task]]\nid = \"t-1\"\nwave = 1\ntitle = \"x\"\nfiles = [\"a.go\"]\ncovers = [\"c-1\"]\nstatus = \"pending\"\n")
	client := newFakeBoard()
	return &Ctx{
		Client:    client,
		Board:     board.New(),
		Proj:      &project.Project{},
		Root:      root,
		BoardPath: filepath.Join(root, board.File),
		Out:       out,
	}, client
}

// TestSyncPhaseCreatesThenUpdates: the first sync creates one issue carrying
// [dross, dross/status:planned, dross/phase:<id>]; the second updates the same
// key and creates nothing. The narration goes to Ctx.Out.
func TestSyncPhaseCreatesThenUpdates(t *testing.T) {
	var out bytes.Buffer
	ctx, client := syncFixture(t, &out)

	if err := SyncPhase(ctx, "auth", "", false); err != nil {
		t.Fatalf("first SyncPhase: %v", err)
	}
	if len(client.created) != 1 || len(client.updated) != 0 {
		t.Fatalf("first sync: created=%d updated=%d, want 1/0", len(client.created), len(client.updated))
	}
	got := append([]string(nil), client.created[0].Labels...)
	sort.Strings(got)
	want := []string{LabelMarker, PhaseLabel("auth"), StatusLabel(StatusPlanned)}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("labels = %v, want %v", got, want)
	}
	key, ok := ctx.Board.PhaseIssue("auth")
	if !ok || key == "" {
		t.Fatal("board.json did not record the phase's issue")
	}
	if !strings.Contains(out.String(), "phase auth -> board "+key+" ("+StatusPlanned+")") {
		t.Errorf("narration = %q", out.String())
	}
	if _, err := os.Stat(ctx.BoardPath); err != nil {
		t.Errorf("board.json not saved: %v", err)
	}

	out.Reset()
	if err := SyncPhase(ctx, "auth", StatusInProgress, false); err != nil {
		t.Fatalf("second SyncPhase: %v", err)
	}
	if len(client.created) != 1 {
		t.Errorf("second sync created another issue (%d total) — the phase label must resolve the existing one", len(client.created))
	}
	if !reflect.DeepEqual(client.updated, []string{key}) {
		t.Errorf("second sync updated %v, want [%s]", client.updated, key)
	}
	if iss := client.issues[key]; !contains(iss.Labels, StatusLabel(StatusInProgress)) || contains(iss.Labels, StatusLabel(StatusPlanned)) {
		t.Errorf("labels after update = %v, want the status relabelled wholesale", iss.Labels)
	}
	if !strings.Contains(out.String(), "("+StatusInProgress+")") {
		t.Errorf("second narration = %q", out.String())
	}
}

// TestSyncPhaseResolvesFromTheTrackerWithoutBoardJSON: board.json dies with
// the phase branch, so a sync on a machine that never had it must find the
// issue by its phase label rather than minting a duplicate.
func TestSyncPhaseResolvesFromTheTrackerWithoutBoardJSON(t *testing.T) {
	var out bytes.Buffer
	ctx, client := syncFixture(t, &out)
	if err := SyncPhase(ctx, "auth", "", false); err != nil {
		t.Fatal(err)
	}
	ctx.Board = board.New() // a fresh clone's empty board.json
	if err := SyncPhase(ctx, "auth", "", false); err != nil {
		t.Fatal(err)
	}
	if len(client.created) != 1 {
		t.Errorf("a sync without board.json minted a duplicate (%d issues)", len(client.created))
	}
}

// TestSyncPhaseCloseGoesThroughOneVerifiedPath: --close on either edge closes
// the same key, narrates "(closed)", and re-reads the issue to prove it.
func TestSyncPhaseCloseGoesThroughOneVerifiedPath(t *testing.T) {
	var out bytes.Buffer
	ctx, client := syncFixture(t, &out)
	if err := SyncPhase(ctx, "auth", "complete", true); err != nil {
		t.Fatalf("close on create: %v", err)
	}
	if len(client.closed) != 1 || client.issues[client.closed[0]].State != "closed" {
		t.Errorf("close on the create edge did not close: %v", client.closed)
	}
	if !strings.Contains(out.String(), "(closed)") {
		t.Errorf("narration = %q", out.String())
	}
}

// TestCtxOutDefaultsToStdoutAtCallTime: a nil Out resolves os.Stdout when
// out() is CALLED, so a test that swaps os.Stdout for a pipe after package
// init still captures the narration.
func TestCtxOutDefaultsToStdoutAtCallTime(t *testing.T) {
	var c *Ctx
	if c.out() != os.Stdout {
		t.Error("nil Ctx did not resolve os.Stdout")
	}
	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	got := (&Ctx{}).out()
	os.Stdout = prev
	w.Close()
	r.Close()
	if got != w {
		t.Error("out() resolved os.Stdout at init rather than at call time")
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
