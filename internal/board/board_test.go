package board

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	b, err := Load(filepath.Join(t.TempDir(), "board.json"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if b.Milestones == nil || b.Phases == nil || b.Quicks == nil {
		t.Error("maps should be initialised on a fresh board")
	}
	if len(b.Phases) != 0 {
		t.Error("fresh board should have no links")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.json")
	b := New()
	b.SetMilestone("v0.2", 7)
	b.SetPhase("02-auth", 12)
	b.SetQuick("0.2.3.5", 18)
	b.Dismiss(21)
	b.MarkPulled()
	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id, ok := got.MilestoneID("v0.2"); !ok || id != 7 {
		t.Errorf("milestone v0.2 = %d,%v", id, ok)
	}
	if n, ok := got.PhaseIssue("02-auth"); !ok || n != 12 {
		t.Errorf("phase 02-auth = %d,%v", n, ok)
	}
	if n, ok := got.QuickIssue("0.2.3.5"); !ok || n != 18 {
		t.Errorf("quick = %d,%v", n, ok)
	}
	if !got.IsDismissed(21) {
		t.Error("dismissed 21 not persisted")
	}
	if got.LastPull.IsZero() {
		t.Error("last_pull not persisted")
	}
}

func TestMissingLookups(t *testing.T) {
	b := New()
	if _, ok := b.MilestoneID("nope"); ok {
		t.Error("unset milestone reported as linked")
	}
	if _, ok := b.PhaseIssue("nope"); ok {
		t.Error("unset phase reported as linked")
	}
	if _, ok := b.QuickIssue("nope"); ok {
		t.Error("unset quick reported as linked")
	}
}

func TestDismissIdempotent(t *testing.T) {
	b := New()
	b.Dismiss(5)
	b.Dismiss(5)
	if len(b.Dismissed) != 1 {
		t.Errorf("Dismissed = %v, want one entry", b.Dismissed)
	}
	if !b.IsDismissed(5) || b.IsDismissed(6) {
		t.Error("IsDismissed wrong")
	}
}

func TestIsLinkedExcludesMilestones(t *testing.T) {
	b := New()
	b.SetMilestone("v0.2", 7) // milestone id 7 — NOT an issue number
	b.SetPhase("02-auth", 12)
	b.SetQuick("q", 18)

	if !b.IsLinked(12) || !b.IsLinked(18) {
		t.Error("phase/quick issue numbers should be linked")
	}
	if b.IsLinked(7) {
		t.Error("milestone id must not count as a linked issue number")
	}
	if b.IsLinked(99) {
		t.Error("unrelated issue reported as linked")
	}
}

func TestLoadUnmarshalsNilMapsSafely(t *testing.T) {
	// A board.json written with only some keys (or by an older version)
	// must still come back with usable maps.
	path := filepath.Join(t.TempDir(), "board.json")
	if err := os.WriteFile(path, []byte(`{"phases":{"01-x":3}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	b.SetMilestone("v1", 1) // would panic on a nil map
	if n, ok := b.PhaseIssue("01-x"); !ok || n != 3 {
		t.Errorf("phase 01-x = %d,%v", n, ok)
	}
}
