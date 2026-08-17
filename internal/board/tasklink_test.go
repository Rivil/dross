package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTaskLinkReadsTheLegacyStringShape is the migration.
//
// board.json is git-tracked, so a file written before the ledger existed
// arrives by pulling a branch, not only by sitting on disk. Failing to decode
// it would break `dross issue` for anyone who had ever synced a task.
func TestTaskLinkReadsTheLegacyStringShape(t *testing.T) {
	legacy := `{"milestones":{},"phases":{},"quicks":{},"tasks":{"p1/t-1":"PROJ-7"}}`
	path := filepath.Join(t.TempDir(), "board.json")
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Load(path)
	if err != nil {
		t.Fatalf("a pre-ledger board.json must still load: %v", err)
	}
	got, ok := b.TaskIssue("p1", "t-1")
	if !ok || got != "PROJ-7" {
		t.Errorf("issue = %q/%v, want PROJ-7", got, ok)
	}
	// No agreement point is the honest reading — the pair was never recorded,
	// so nothing is known about what either side held.
	link, _ := b.TaskLinkFor("p1", "t-1")
	if link.PlanStatus != "" || link.BoardState != "" {
		t.Errorf("migrated link invented an agreement point: %+v", link)
	}
}

func TestTaskLinkRoundTripsTheRecordShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.json")
	b := New()
	b.SetTaskSynced("p1", "t-1", "PROJ-7", "in_progress", "task-in-progress")
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	link, ok := reloaded.TaskLinkFor("p1", "t-1")
	if !ok {
		t.Fatal("link missing after round trip")
	}
	if link.Issue != "PROJ-7" || link.PlanStatus != "in_progress" || link.BoardState != "task-in-progress" {
		t.Errorf("link = %+v, want the full agreement point", link)
	}
}

// TestSetTaskPreservesTheAgreementPoint: re-resolving a stale issue id must not
// discard the snapshot, or the next pull treats a moved card as a first sync
// and applies it with no conflict check.
func TestSetTaskPreservesTheAgreementPoint(t *testing.T) {
	b := New()
	b.SetTaskSynced("p1", "t-1", "PROJ-7", "in_progress", "task-in-progress")
	b.SetTask("p1", "t-1", "PROJ-9") // a re-resolve after a cache miss

	link, _ := b.TaskLinkFor("p1", "t-1")
	if link.Issue != "PROJ-9" {
		t.Errorf("issue = %q, want the re-resolved PROJ-9", link.Issue)
	}
	if link.PlanStatus != "in_progress" || link.BoardState != "task-in-progress" {
		t.Errorf("agreement point lost on re-resolve: %+v", link)
	}
}

// TestLegacyBoardSurvivesAReSave: loading an old file and saving it must not
// corrupt the entries it did not touch.
func TestLegacyBoardSurvivesAReSave(t *testing.T) {
	legacy := `{"milestones":{},"phases":{},"quicks":{},"tasks":{"p1/t-1":"PROJ-7","p2/t-1":"PROJ-8"}}`
	path := filepath.Join(t.TempDir(), "board.json")
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Tasks map[string]struct {
			Issue string `json:"issue"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("re-saved board is not the record shape: %v", err)
	}
	if probe.Tasks["p1/t-1"].Issue != "PROJ-7" || probe.Tasks["p2/t-1"].Issue != "PROJ-8" {
		t.Errorf("entries lost across the migration: %+v", probe.Tasks)
	}
}
