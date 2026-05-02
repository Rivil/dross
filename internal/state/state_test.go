package state

import (
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := New()
	s.Version = "1.2.3.4"
	s.CurrentMilestone = "v1.2"
	s.CurrentPhase = "03-meal-tagging"
	s.CurrentPhaseStatus = "executing"
	s.Touch("created phase")

	if err := s.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Version != "1.2.3.4" {
		t.Errorf("version: got %q want %q", got.Version, "1.2.3.4")
	}
	if got.CurrentPhase != "03-meal-tagging" {
		t.Errorf("phase: got %q", got.CurrentPhase)
	}
	if len(got.History) != 1 {
		t.Fatalf("history len: got %d want 1", len(got.History))
	}
	if got.History[0].Action != "created phase" {
		t.Errorf("history action: got %q", got.History[0].Action)
	}
}

func TestTouchCapsHistory(t *testing.T) {
	s := New()
	for i := 0; i < 60; i++ {
		s.Touch("a")
	}
	if len(s.History) != 50 {
		t.Errorf("expected history capped at 50, got %d", len(s.History))
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}
