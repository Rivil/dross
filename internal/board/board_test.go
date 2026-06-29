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
	b.SetMilestone("v0.2", "7")
	b.SetPhase("02-auth", "12")
	b.SetQuick("0.2.3.5", "18")
	b.Dismiss("21")
	b.MarkPulled()
	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id, ok := got.MilestoneID("v0.2"); !ok || id != "7" {
		t.Errorf("milestone v0.2 = %q,%v", id, ok)
	}
	if n, ok := got.PhaseIssue("02-auth"); !ok || n != "12" {
		t.Errorf("phase 02-auth = %q,%v", n, ok)
	}
	if n, ok := got.QuickIssue("0.2.3.5"); !ok || n != "18" {
		t.Errorf("quick = %q,%v", n, ok)
	}
	if !got.IsDismissed("21") {
		t.Error("dismissed 21 not persisted")
	}
	if got.LastPull.IsZero() {
		t.Error("last_pull not persisted")
	}
}

// TestBoardReadableIDRoundTrip proves c-1/c-4: links are keyed by readable
// string issue ids (e.g. YouTrack "PROJ-123") across Phases, Quicks AND
// Milestones, with Dismissed a string set — all surviving Save→Load.
func TestBoardReadableIDRoundTrip(t *testing.T) {
	b := New()
	b.SetPhase("02-auth", "PROJ-123")
	b.SetQuick("0.2.3.5", "PROJ-200")
	b.SetMilestone("v0.2", "PROJ-9")
	b.Dismiss("PROJ-300")

	check := func(b *Board, when string) {
		if v, ok := b.PhaseIssue("02-auth"); !ok || v != "PROJ-123" {
			t.Errorf("%s: phase = %q,%v want PROJ-123", when, v, ok)
		}
		if v, ok := b.QuickIssue("0.2.3.5"); !ok || v != "PROJ-200" {
			t.Errorf("%s: quick = %q,%v want PROJ-200", when, v, ok)
		}
		if v, ok := b.MilestoneID("v0.2"); !ok || v != "PROJ-9" {
			t.Errorf("%s: milestone = %q,%v want PROJ-9", when, v, ok)
		}
		if !b.IsLinked("PROJ-123") || !b.IsLinked("PROJ-200") {
			t.Errorf("%s: phase/quick readable ids should be linked", when)
		}
		if b.IsLinked("PROJ-9") {
			t.Errorf("%s: milestone id must not count as a linked issue", when)
		}
		if !b.IsDismissed("PROJ-300") {
			t.Errorf("%s: dismissed readable id not tracked", when)
		}
	}

	check(b, "in-memory")

	path := filepath.Join(t.TempDir(), File)
	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	check(loaded, "round-tripped")
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
	b.Dismiss("5")
	b.Dismiss("5")
	if len(b.Dismissed) != 1 {
		t.Errorf("Dismissed = %v, want one entry", b.Dismissed)
	}
	if !b.IsDismissed("5") || b.IsDismissed("6") {
		t.Error("IsDismissed wrong")
	}
}

func TestIsLinkedExcludesMilestones(t *testing.T) {
	b := New()
	b.SetMilestone("v0.2", "7") // milestone id 7 — NOT an issue id
	b.SetPhase("02-auth", "12")
	b.SetQuick("q", "18")

	if !b.IsLinked("12") || !b.IsLinked("18") {
		t.Error("phase/quick issue ids should be linked")
	}
	if b.IsLinked("7") {
		t.Error("milestone id must not count as a linked issue id")
	}
	if b.IsLinked("99") {
		t.Error("unrelated issue reported as linked")
	}
}

func TestLoadUnmarshalsNilMapsSafely(t *testing.T) {
	// A board.json written with only some keys (or by an older version)
	// must still come back with usable maps.
	path := filepath.Join(t.TempDir(), "board.json")
	if err := os.WriteFile(path, []byte(`{"phases":{"01-x":"3"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	b.SetMilestone("v1", "1") // would panic on a nil map
	if n, ok := b.PhaseIssue("01-x"); !ok || n != "3" {
		t.Errorf("phase 01-x = %q,%v", n, ok)
	}
}
