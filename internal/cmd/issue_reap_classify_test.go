package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/boardsync"
)

// The classify tests moved with Classify to internal/boardsync
// (reap_classify_test.go). readOnlyYT, reapRepo and writeChanges stay here
// because the reap command, doctor and watch tests drive the cobra path over
// them; TestQuickIsNeverAutoClosed stays for the same reason.

// readOnlyYT is a YouTrack stand-in that serves reads and FAILS THE TEST on any
// request that is not a GET.
//
// That inversion is the point of the fixture, not a detail of it: classify is
// the half of reap that must be safe to run on a live board at any time, so a
// classifier that reached for a write — or that decided a verdict by patching a
// card and seeing what stuck — has to redden here rather than on someone's
// tracker. `resolved` names the cards the tracker already holds done.
type readOnlyYT struct {
	resolved map[string]bool
	gets     int
}

func (f *readOnlyYT) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("classify issued a %s to %s — classification is read-only", r.Method, r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		switch {
		case r.URL.Path == "/api/issueTags":
			_, _ = io.WriteString(w, `[]`)
		case r.URL.Path == "/api/issues":
			_, _ = io.WriteString(w, `[]`)
		case strings.HasPrefix(r.URL.Path, "/api/issues/"):
			f.gets++
			key := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			resolved := "null"
			if f.resolved[key] {
				resolved = "1700000000000"
			}
			_, _ = io.WriteString(w, `{"idReadable":"`+key+`","resolved":`+resolved+`}`)
		default:
			t.Errorf("unexpected GET %s", r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
		}
	}
}

// reapRepo scaffolds a YouTrack board in epic mode — the one shape whose
// milestone slot holds an issue — with the given board.json, and returns the
// repo dir plus a live boardCtx.
func reapRepo(t *testing.T, f *readOnlyYT, boardJSON string) (string, *boardCtx) {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	dir := youtrackBoardRepo(t, srv.URL)
	mustRunSet(t, "board.milestone_mode", "epic")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), boardJSON)
	ctx, enabled, err := openBoard()
	if err != nil {
		t.Fatalf("openBoard: %v", err)
	}
	if !enabled {
		t.Fatal("board sync must be enabled in the fixture")
	}
	return dir, ctx
}

// writeChanges writes a phase's completion record. status "" writes a record
// with no status field at all — the pre-status shape, which reads as "unknown",
// never as "done".
func writeChanges(t *testing.T, dir, slug, status string) {
	t.Helper()
	rec := map[string]any{"phase": slug, "tasks": map[string]any{}}
	if status != "" {
		rec["status"] = status
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "phases", slug, "changes.json"), string(b))
}

// TestQuickIsNeverAutoClosed: version ordering is not a completion record.
// state.json's counter proves a LATER bump happened, which is equally true of a
// quick that shipped and one abandoned halfway.
func TestQuickIsNeverAutoClosed(t *testing.T) {
	f := &readOnlyYT{resolved: map[string]bool{}}
	_, ctx := reapRepo(t, f, `{"phases":{},"tasks":{},"milestones":{},"quicks":{"1.0.0.1":"PROJ-40"}}`)
	// The repo's own version is well past the quick's ref.
	if err := runCmd(t, State(), "set", "version", "1.5.4.0"); err != nil {
		t.Fatalf("set version: %v", err)
	}

	plan, err := boardsync.Classify(ctx, nil)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if hasKey(plan.Cards, "PROJ-40") {
		t.Error("a quick was classified stranded on version ordering alone — that is not a completion record")
	}
	if !hasKey(plan.Unattributable, "PROJ-40") {
		t.Errorf("the quick was dropped rather than named for a human; unattributable = %v", cardKeys(plan.Unattributable))
	}
}

func hasKey(cards []boardsync.ReapCard, key string) bool {
	for _, c := range cards {
		if c.Key == key {
			return true
		}
	}
	return false
}

func cardKeys(cards []boardsync.ReapCard) []string {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		out = append(out, c.Key)
	}
	return out
}
